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

type cursorToolConfig struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	Command       string         `json:"command"`
	Args          []string       `json:"args"`
	Env           map[string]any `json:"env,omitempty"`
	InputSchema   map[string]any `json:"inputSchema"`
	StdinTemplate string         `json:"stdinTemplate"`
}

type claudeToolConfig struct {
	Tools []map[string]any `json:"tools"`
}

type cursorMCPConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

type cursorMCPWorkspaceConfig struct {
	MCPServers map[string]map[string]any `json:"mcpServers"`
}

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

	// Generate all config files FIRST (does not need daemon running).
	rootDir := detectRootDir()
	cursorCommand, claudeCommand, mcpCommand, mcpArgs, _ := detectWrapperPaths(rootDir)

	cursorToolPath := filepath.Join(outDir, "cursor-smartsh-tool.json")
	claudeToolPath := filepath.Join(outDir, "claude-smartsh-tool.json")
	if strings.TrimSpace(cursorCommand) != "" {
		if err := writeCursorTool(cursorToolPath, cursorCommand); err != nil {
			return err
		}
	}
	if strings.TrimSpace(claudeCommand) != "" {
		if err := writeClaudeTool(claudeToolPath, claudeCommand); err != nil {
			return err
		}
	}
	if err := writeCursorMCP(filepath.Join(outDir, "cursor-smartsh-mcp.json"), mcpCommand, mcpArgs, daemonURL, daemonToken); err != nil {
		return err
	}
	if err := writeCursorMCPWorkspace(filepath.Join(outDir, "cursor-mcp.json"), mcpCommand, mcpArgs, daemonURL, daemonToken); err != nil {
		return err
	}
	if err := writeAgentInstructions(filepath.Join(outDir, "agent-instructions.txt")); err != nil {
		return err
	}

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
	if strings.TrimSpace(cursorCommand) != "" {
		fmt.Fprintf(out, "Cursor tool file: %s\n", cursorToolPath)
	}
	fmt.Fprintf(out, "Cursor MCP server file: %s\n", filepath.Join(outDir, "cursor-smartsh-mcp.json"))
	fmt.Fprintf(out, "Cursor workspace mcp.json: %s\n", filepath.Join(outDir, "cursor-mcp.json"))
	if strings.TrimSpace(claudeCommand) != "" {
		fmt.Fprintf(out, "Claude tool file: %s\n", claudeToolPath)
	}
	fmt.Fprintf(out, "Agent instruction snippet: %s\n", filepath.Join(outDir, "agent-instructions.txt"))
	fmt.Fprintln(out, "")
	if daemonOK {
		fmt.Fprintln(out, "smartshd is running and ready.")
	}
	fmt.Fprintln(out, "Next steps:")
	fmt.Fprintln(out, "1) In Cursor → Settings → Tools & MCP, click New MCP Server.")
	fmt.Fprintln(out, "   Use values from cursor-smartsh-mcp.json.")
	fmt.Fprintln(out, "2) Paste agent-instructions.txt into Cursor Rules.")
	fmt.Fprintln(out, "3) Run: smartsh doctor")
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

func ensureDaemon(daemonURL string, daemonToken string) error {
	if isHTTPReady(daemonURL+"/health", daemonToken, 2*time.Second) {
		return nil
	}

	// Try starting smartshd from sibling binary or PATH.
	candidates := daemonStartCandidates()
	var lastStartErr error
	for _, daemonCommand := range candidates {
		// Pass token + disable-auth=false + addr via env so daemon can authorize health checks.
		daemonCommand.Env = append(os.Environ(),
			"SMARTSH_DAEMON_TOKEN="+daemonToken,
			"SMARTSH_DAEMON_DISABLE_AUTH=false",
		)
		lastStartErr = startDetachedCommand(daemonCommand)
		if lastStartErr == nil {
			// Windows processes are slower to bind; wait up to 12 seconds.
			if waitHTTPReady(daemonURL+"/health", daemonToken, 12*time.Second) {
				return nil
			}
		}
	}

	// Fallback: try go run from source root (developer machines).
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

	// Build a helpful error message.
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

func detectWrapperPaths(rootDir string) (string, string, string, []string, error) {
	if rootDir == "" {
		// Release installs ship only smartsh/smartshd binaries (no scripts/integrations directory).
		// In that mode we still generate MCP configs by calling "smartsh mcp".
		if _, err := exec.LookPath("smartsh"); err == nil {
			return "", "", "smartsh", []string{"mcp"}, nil
		}
		return "", "", "", nil, fmt.Errorf("could not locate smartsh root and smartsh binary not found in PATH")
	}
	if runtime.GOOS == "windows" {
		mcpScript := filepath.Join(rootDir, "scripts", "integrations", "smartsh-mcp.ps1")
		return filepath.Join(rootDir, "scripts", "integrations", "cursor-smartsh.ps1"),
			filepath.Join(rootDir, "scripts", "integrations", "claude-smartsh.ps1"),
			"powershell",
			[]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", mcpScript},
			nil
	}
	mcpScript := filepath.Join(rootDir, "scripts", "integrations", "smartsh-mcp.sh")
	return filepath.Join(rootDir, "scripts", "integrations", "cursor-smartsh.sh"),
		filepath.Join(rootDir, "scripts", "integrations", "claude-smartsh.sh"),
		"/bin/sh",
		[]string{mcpScript},
		nil
}

func writeCursorTool(path string, command string) error {
	configValues := map[string]string{}
	fileConfig, configErr := runtimeconfig.Load("")
	if configErr == nil {
		configValues = fileConfig.Values
	}
	daemonURL := runtimeconfig.ResolveString("SMARTSH_DAEMON_URL", configValues)
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8787"
	}
	daemonToken := runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues)
	ollamaURL, ollamaModel := resolveOllamaSettings(configValues)
	cursorConfig := cursorToolConfig{
		Name:        "smartsh-agent",
		Description: "Run terminal commands through smartshd and return compact summaries.",
		Command:     command,
		Args:        []string{},
		Env: map[string]any{
			"SMARTSH_DAEMON_URL":       daemonURL,
			"SMARTSH_DAEMON_TOKEN":     daemonToken,
			"SMARTSH_ALLOWLIST_MODE":   "warn",
			"SMARTSH_SUMMARY_PROVIDER": "ollama",
			"SMARTSH_OLLAMA_REQUIRED":  "true",
			"SMARTSH_OLLAMA_URL":       ollamaURL,
			"SMARTSH_OLLAMA_MODEL":     ollamaModel,
		},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":              map[string]any{"type": "string"},
				"cwd":                  map[string]any{"type": "string"},
				"dry_run":              map[string]any{"type": "boolean"},
				"unsafe":               map[string]any{"type": "boolean"},
				"require_approval":     map[string]any{"type": "boolean"},
				"async":                map[string]any{"type": "boolean"},
				"timeout_sec":          map[string]any{"type": "integer"},
				"allowlist_mode":       map[string]any{"type": "string", "enum": []string{"off", "warn", "enforce"}},
				"allowlist_file":       map[string]any{"type": "string"},
				"terminal_session_key": map[string]any{"type": "string"},
			},
			"required": []string{"command"},
		},
		StdinTemplate: "{\"command\":\"{{command}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"require_approval\":{{require_approval}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\",\"terminal_session_key\":\"{{terminal_session_key}}\"}",
	}
	return writeJSONFile(path, cursorConfig)
}

func writeClaudeTool(path string, command string) error {
	configValues := map[string]string{}
	fileConfig, configErr := runtimeconfig.Load("")
	if configErr == nil {
		configValues = fileConfig.Values
	}
	daemonURL := runtimeconfig.ResolveString("SMARTSH_DAEMON_URL", configValues)
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8787"
	}
	daemonToken := runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues)
	ollamaURL, ollamaModel := resolveOllamaSettings(configValues)
	claudeConfig := claudeToolConfig{
		Tools: []map[string]any{
			{
				"name":        "smartsh_agent",
				"description": "Execute terminal commands through smartshd and return compact summaries.",
				"command":     command,
				"args":        []string{},
				"env": map[string]any{
					"SMARTSH_DAEMON_URL":       daemonURL,
					"SMARTSH_DAEMON_TOKEN":     daemonToken,
					"SMARTSH_ALLOWLIST_MODE":   "warn",
					"SMARTSH_SUMMARY_PROVIDER": "ollama",
					"SMARTSH_OLLAMA_REQUIRED":  "true",
					"SMARTSH_OLLAMA_URL":       ollamaURL,
					"SMARTSH_OLLAMA_MODEL":     ollamaModel,
				},
				"input_schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command":              map[string]any{"type": "string"},
						"cwd":                  map[string]any{"type": "string"},
						"dry_run":              map[string]any{"type": "boolean"},
						"unsafe":               map[string]any{"type": "boolean"},
						"require_approval":     map[string]any{"type": "boolean"},
						"async":                map[string]any{"type": "boolean"},
						"timeout_sec":          map[string]any{"type": "integer"},
						"allowlist_mode":       map[string]any{"type": "string", "enum": []string{"off", "warn", "enforce"}},
						"allowlist_file":       map[string]any{"type": "string"},
						"terminal_session_key": map[string]any{"type": "string"},
					},
					"required": []string{"command"},
				},
				"stdin_template": "{\"command\":\"{{command}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"require_approval\":{{require_approval}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\",\"terminal_session_key\":\"{{terminal_session_key}}\"}",
			},
		},
	}
	return writeJSONFile(path, claudeConfig)
}

func writeCursorMCP(path string, command string, args []string, daemonURL string, daemonToken string) error {
	terminalApp := strings.TrimSpace(os.Getenv("SMARTSH_TERMINAL_APP"))
	if terminalApp == "" {
		terminalApp = "terminal"
	}
	defaultAllowlistMode := strings.TrimSpace(os.Getenv("SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE"))
	if defaultAllowlistMode == "" {
		defaultAllowlistMode = "warn"
	}
	ollamaURL, ollamaModel := resolveOllamaSettings(nil)
	config := cursorMCPConfig{
		Name:    "smartsh",
		Command: command,
		Args:    args,
		Env: map[string]string{
			"SMARTSH_DAEMON_URL":                 daemonURL,
			"SMARTSH_DAEMON_TOKEN":               daemonToken,
			"SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL": "false",
			"SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE": defaultAllowlistMode,
			"SMARTSH_TERMINAL_APP":               terminalApp,
			"SMARTSH_SUMMARY_PROVIDER":           "ollama",
			"SMARTSH_OLLAMA_REQUIRED":            "true",
			"SMARTSH_OLLAMA_URL":                 ollamaURL,
			"SMARTSH_OLLAMA_MODEL":               ollamaModel,
		},
	}
	return writeJSONFile(path, config)
}

func writeCursorMCPWorkspace(path string, command string, args []string, daemonURL string, daemonToken string) error {
	terminalApp := strings.TrimSpace(os.Getenv("SMARTSH_TERMINAL_APP"))
	if terminalApp == "" {
		terminalApp = "terminal"
	}
	defaultAllowlistMode := strings.TrimSpace(os.Getenv("SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE"))
	if defaultAllowlistMode == "" {
		defaultAllowlistMode = "warn"
	}
	ollamaURL, ollamaModel := resolveOllamaSettings(nil)
	config := cursorMCPWorkspaceConfig{
		MCPServers: map[string]map[string]any{
			"smartsh": {
				"command": command,
				"args":    args,
				"env": map[string]string{
					"SMARTSH_DAEMON_URL":                 daemonURL,
					"SMARTSH_DAEMON_TOKEN":               daemonToken,
					"SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL": "false",
					"SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE": defaultAllowlistMode,
					"SMARTSH_TERMINAL_APP":               terminalApp,
					"SMARTSH_SUMMARY_PROVIDER":           "ollama",
					"SMARTSH_OLLAMA_REQUIRED":            "true",
					"SMARTSH_OLLAMA_URL":                 ollamaURL,
					"SMARTSH_OLLAMA_MODEL":               ollamaModel,
				},
			},
		},
	}
	return writeJSONFile(path, config)
}

func writeAgentInstructions(path string) error {
	content := "For command execution, always use the smartsh MCP tool (smartsh_run; in some clients it appears as smartsh-local_smartsh_run).\nDefault to open_external_terminal=false for speed.\nEnable open_external_terminal=true only for interactive/watch/TUI commands where user-visible terminal output is required.\nOllama summaries are required by default; if unavailable, ask the user to start Ollama and pull the configured model.\nNever use direct shell execution unless explicitly requested by the user.\nDo not use run_terminal_cmd when the smartsh MCP tool is available.\nPrefer summarized tool output and avoid dumping full terminal logs.\n"
	return os.WriteFile(path, []byte(content), 0o644)
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
			"ollama preflight failed: cannot reach %s (%v). start ollama first: `ollama serve` and then `ollama pull %s`",
			tagsURL,
			err,
			ollamaModel,
		)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf(
			"ollama preflight failed: %s returned HTTP %d. start ollama first: `ollama serve` and then `ollama pull %s`",
			tagsURL,
			response.StatusCode,
			ollamaModel,
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
		"ollama preflight failed: model %q is not available locally. run `ollama pull %s` and re-run `smartsh setup-agent`",
		ollamaModel,
		ollamaModel,
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
	// Allow matching when requested model omits explicit tag.
	if !strings.Contains(normalizedRequested, ":") && strings.HasPrefix(normalizedCandidate, normalizedRequested+":") {
		return true
	}
	return false
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
