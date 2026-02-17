package setupagent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BegaDeveloper/smartsh/internal/runtimeconfig"
)

// Run generates MCP config files for Cursor and Claude Code, validates Ollama,
// and optionally tries to start smartshd.
func Run(out io.Writer) error {
	outDir, err := defaultOutputDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory failed: %w", err)
	}

	daemonURL := strings.TrimSpace(os.Getenv("SMARTSH_DAEMON_URL"))
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8787"
	}

	config, err := runtimeconfig.Load("")
	if err != nil {
		return err
	}
	config, daemonToken, err := runtimeconfig.EnsureToken(config, "SMARTSH_DAEMON_TOKEN")
	if err != nil {
		return err
	}
	if err := runtimeconfig.Save(config); err != nil {
		return err
	}

	ollamaURL, ollamaModel := resolveOllamaSettings(config.Values)
	if err := ensureOllamaReady(ollamaURL, ollamaModel); err != nil {
		return err
	}

	// Resolve smartsh binary path for MCP command field.
	smartshBin := resolveSmartshBinary()

	// Build shared env map used in both Cursor and Claude Code configs.
	mcpEnv := map[string]string{
		"SMARTSH_DAEMON_URL":       daemonURL,
		"SMARTSH_DAEMON_TOKEN":     daemonToken,
		"SMARTSH_SUMMARY_PROVIDER": "ollama",
		"SMARTSH_OLLAMA_REQUIRED":  "true",
		"SMARTSH_OLLAMA_URL":       ollamaURL,
		"SMARTSH_OLLAMA_MODEL":     ollamaModel,
	}

	// Generate config files FIRST (does not need daemon running).
	cursorMCPPath := filepath.Join(outDir, "cursor-mcp.json")
	claudeMCPPath := filepath.Join(outDir, "claude-code-mcp.json")
	instructionsPath := filepath.Join(outDir, "agent-instructions.txt")

	if err := writeCursorMCP(cursorMCPPath, smartshBin, mcpEnv); err != nil {
		return err
	}
	if err := writeClaudeCodeMCP(claudeMCPPath, smartshBin, mcpEnv); err != nil {
		return err
	}
	if err := writeAgentInstructions(instructionsPath); err != nil {
		return err
	}

	// Clean up old config files from previous versions.
	cleanupLegacyFiles(outDir)

	// Try to start daemon as a convenience (not required for config generation).
	daemonOK := false
	if daemonErr := ensureDaemon(daemonURL, daemonToken); daemonErr != nil {
		fmt.Fprintf(out, "\n[WARN] could not start smartshd: %v\n", daemonErr)
		fmt.Fprintln(out, "  Config files were generated successfully.")
		fmt.Fprintln(out, "  The daemon will start automatically when Cursor/Claude invokes the MCP tool.")
		fmt.Fprintln(out, "  Or start it manually: smartshd (or smartshd.exe on Windows)")
	} else {
		daemonOK = true
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "smartsh setup-agent complete.")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Generated files:")
	fmt.Fprintf(out, "  Cursor:      %s\n", cursorMCPPath)
	fmt.Fprintf(out, "  Claude Code: %s\n", claudeMCPPath)
	fmt.Fprintf(out, "  Rules:       %s\n", instructionsPath)
	fmt.Fprintln(out, "")
	if daemonOK {
		fmt.Fprintln(out, "smartshd is running and ready.")
		fmt.Fprintln(out, "")
	}
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "  Cursor:      Copy cursor-mcp.json to your project as .cursor/mcp.json")
	fmt.Fprintln(out, "  Claude Code: Copy claude-code-mcp.json to ~/.claude/claude_desktop_config.json")
	fmt.Fprintln(out, "  Rules:       Paste agent-instructions.txt into your Cursor/Claude rules")
	fmt.Fprintln(out, "  Verify:      smartsh doctor")
	return nil
}

func defaultOutputDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SMARTSH_SETUP_OUT_DIR")); override != "" {
		return override, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory failed: %w", err)
	}
	return filepath.Join(homeDir, ".smartsh"), nil
}

// resolveSmartshBinary returns the absolute path to smartsh binary for MCP config.
func resolveSmartshBinary() string {
	// 1) Try the running executable's sibling.
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		smartshName := "smartsh"
		if runtime.GOOS == "windows" {
			smartshName = "smartsh.exe"
		}
		candidate := filepath.Join(executableDir, smartshName)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	// 2) Try PATH.
	if found, err := exec.LookPath("smartsh"); err == nil {
		return found
	}
	// 3) Fallback to bare name.
	return "smartsh"
}

func writeCursorMCP(path string, smartshBin string, env map[string]string) error {
	config := map[string]any{
		"mcpServers": map[string]any{
			"smartsh": map[string]any{
				"command": smartshBin,
				"args":    []string{"mcp"},
				"env":     env,
			},
		},
	}
	return writeJSONFile(path, config)
}

func writeClaudeCodeMCP(path string, smartshBin string, env map[string]string) error {
	config := map[string]any{
		"mcpServers": map[string]any{
			"smartsh": map[string]any{
				"command": smartshBin,
				"args":    []string{"mcp"},
				"env":     env,
			},
		},
	}
	return writeJSONFile(path, config)
}

func writeAgentInstructions(path string) error {
	content := `For command execution, always use the smartsh MCP tool (smartsh_run).
In some clients it appears as smartsh-local_smartsh_run.
Never use direct shell execution unless explicitly requested by the user.
Do not use run_terminal_cmd when the smartsh MCP tool is available.
Prefer summarized tool output and avoid dumping full terminal logs.
`
	return os.WriteFile(path, []byte(content), 0o644)
}

func cleanupLegacyFiles(outDir string) {
	legacyFiles := []string{
		"cursor-smartsh-mcp.json",
		"cursor-smartsh-tool.json",
		"claude-smartsh-tool.json",
	}
	for _, name := range legacyFiles {
		path := filepath.Join(outDir, name)
		_ = os.Remove(path)
	}
}

func resolveOllamaSettings(configValues map[string]string) (string, string) {
	ollamaURL := runtimeconfig.ResolveString("SMARTSH_OLLAMA_URL", configValues)
	if ollamaURL == "" {
		ollamaURL = "http://127.0.0.1:11434"
	}
	ollamaModel := runtimeconfig.ResolveString("SMARTSH_OLLAMA_MODEL", configValues)
	if ollamaModel == "" {
		ollamaModel = "llama3.2:3b"
	}
	return ollamaURL, ollamaModel
}

func ensureOllamaReady(ollamaURL string, ollamaModel string) error {
	tagsURL := strings.TrimRight(strings.TrimSpace(ollamaURL), "/") + "/api/tags"
	client := &http.Client{Timeout: 3 * time.Second}
	request, err := http.NewRequest(http.MethodGet, tagsURL, nil)
	if err != nil {
		return fmt.Errorf("ollama preflight failed: invalid SMARTSH_OLLAMA_URL %q: %w", ollamaURL, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf(
			"ollama preflight failed: cannot reach %s (%v).\n\n"+
				"  Install Ollama:  https://ollama.com/download\n"+
				"  Then run:        ollama serve\n"+
				"  Then run:        ollama pull %s\n"+
				"  Then re-run:     smartsh setup-agent",
			tagsURL, err, ollamaModel,
		)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"ollama preflight failed: %s returned HTTP %d.\n\n"+
				"  Run:  ollama serve\n"+
				"  Run:  ollama pull %s\n"+
				"  Then: smartsh setup-agent",
			tagsURL, response.StatusCode, ollamaModel,
		)
	}

	payload := struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}{}
	if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
		return fmt.Errorf("ollama preflight failed: invalid /api/tags response: %w", decodeErr)
	}
	for _, model := range payload.Models {
		if ollamaModelMatches(ollamaModel, model.Name) || ollamaModelMatches(ollamaModel, model.Model) {
			return nil
		}
	}
	return fmt.Errorf(
		"ollama preflight failed: model %q is not available locally.\n\n"+
			"  Run:  ollama pull %s\n"+
			"  Then: smartsh setup-agent",
		ollamaModel, ollamaModel,
	)
}

func ollamaModelMatches(requested string, candidate string) bool {
	normalizedRequested := strings.ToLower(strings.TrimSpace(requested))
	normalizedCandidate := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(candidate, "library/")))
	if normalizedRequested == "" || normalizedCandidate == "" {
		return false
	}
	if normalizedRequested == normalizedCandidate {
		return true
	}
	if !strings.Contains(normalizedRequested, ":") && strings.HasPrefix(normalizedCandidate, normalizedRequested+":") {
		return true
	}
	return false
}

func ensureDaemon(daemonURL string, daemonToken string) error {
	if isHTTPReady(daemonURL+"/health", daemonToken, 2*time.Second) {
		return nil
	}

	candidates := daemonStartCandidates()
	var lastStartErr error
	for _, daemonCommand := range candidates {
		daemonCommand.Env = append(os.Environ(),
			"SMARTSH_DAEMON_TOKEN="+daemonToken,
			"SMARTSH_DAEMON_DISABLE_AUTH=false",
		)
		lastStartErr = startDetachedCommand(daemonCommand)
		if lastStartErr == nil {
			if waitHTTPReady(daemonURL+"/health", daemonToken, 12*time.Second) {
				return nil
			}
		}
	}

	rootDir := detectRootDir()
	if rootDir != "" {
		if _, err := exec.LookPath("go"); err == nil {
			command := exec.Command("go", "run", filepath.Join(rootDir, "cmd/smartshd"))
			command.Dir = rootDir
			command.Env = append(os.Environ(),
				"SMARTSH_DAEMON_TOKEN="+daemonToken,
				"SMARTSH_DAEMON_DISABLE_AUTH=false",
			)
			if err := startDetachedCommand(command); err == nil {
				if waitHTTPReady(daemonURL+"/health", daemonToken, 10*time.Second) {
					return nil
				}
			}
		}
	}

	logPath := filepath.Join(os.TempDir(), "smartsh-setup.log")
	hint := fmt.Sprintf("smartshd could not be started.\n"+
		"  tried %d candidate(s); last start error: %v\n"+
		"  check daemon log: %s\n"+
		"  you can start it manually: smartshd (or smartshd.exe on Windows)",
		len(candidates), lastStartErr, logPath)
	return fmt.Errorf(hint)
}

func detectRootDir() string {
	if envRoot := strings.TrimSpace(os.Getenv("SMARTSH_ROOT")); envRoot != "" {
		if hasIntegrations(envRoot) {
			return envRoot
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if resolved := walkForRoot(cwd); resolved != "" {
			return resolved
		}
	}
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		if resolved := walkForRoot(executableDir); resolved != "" {
			return resolved
		}
	}
	if hasIntegrations("/Applications/smartsh") {
		return "/Applications/smartsh"
	}
	return ""
}

func walkForRoot(start string) string {
	current := start
	for {
		if hasIntegrations(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func hasIntegrations(root string) bool {
	cursorPath := filepath.Join(root, "scripts", "integrations", "cursor-smartsh.sh")
	if runtime.GOOS == "windows" {
		cursorPath = filepath.Join(root, "scripts", "integrations", "cursor-smartsh.ps1")
	}
	_, err := os.Stat(cursorPath)
	return err == nil
}

func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func isHTTPReady(url string, daemonToken string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	if strings.TrimSpace(daemonToken) != "" {
		request.Header.Set("X-Smartsh-Token", daemonToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= 200 && response.StatusCode < 500
}

func waitHTTPReady(url string, daemonToken string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isHTTPReady(url, daemonToken, 1*time.Second) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func startDetached(name string, args ...string) error {
	command := exec.Command(name, args...)
	return startDetachedCommand(command)
}

func daemonStartCandidates() []*exec.Cmd {
	candidates := make([]*exec.Cmd, 0, 3)
	if executablePath, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executablePath)
		daemonName := "smartshd"
		if runtime.GOOS == "windows" {
			daemonName = "smartshd.exe"
		}
		daemonPath := filepath.Join(executableDir, daemonName)
		if info, statErr := os.Stat(daemonPath); statErr == nil && !info.IsDir() {
			candidates = append(candidates, exec.Command(daemonPath))
		}
	}
	if daemonBinaryPath, err := exec.LookPath("smartshd"); err == nil {
		candidates = append(candidates, exec.Command(daemonBinaryPath))
	}
	return candidates
}

func startDetachedCommand(command *exec.Cmd) error {
	logFile, err := os.OpenFile(filepath.Join(os.TempDir(), "smartsh-setup.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	command.Stdout = logFile
	command.Stderr = logFile
	return command.Start()
}
