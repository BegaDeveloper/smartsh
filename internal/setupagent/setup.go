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

	if err := ensureDaemon(daemonURL, daemonToken); err != nil {
		return err
	}

	rootDir := detectRootDir()
	cursorCommand, claudeCommand, mcpCommand, mcpArgs, err := detectWrapperPaths(rootDir)
	if err != nil {
		return err
	}

	if err := writeCursorTool(filepath.Join(outDir, "cursor-smartsh-tool.json"), cursorCommand); err != nil {
		return err
	}
	if err := writeClaudeTool(filepath.Join(outDir, "claude-smartsh-tool.json"), claudeCommand); err != nil {
		return err
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

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "smartsh setup-agent complete.")
	fmt.Fprintf(out, "Cursor tool file: %s\n", filepath.Join(outDir, "cursor-smartsh-tool.json"))
	fmt.Fprintf(out, "Cursor MCP server file: %s\n", filepath.Join(outDir, "cursor-smartsh-mcp.json"))
	fmt.Fprintf(out, "Cursor workspace mcp.json: %s\n", filepath.Join(outDir, "cursor-mcp.json"))
	fmt.Fprintf(out, "Claude tool file: %s\n", filepath.Join(outDir, "claude-smartsh-tool.json"))
	fmt.Fprintf(out, "Agent instruction snippet: %s\n", filepath.Join(outDir, "agent-instructions.txt"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Minimal next step:")
	fmt.Fprintln(out, "1) In Cursor Tools & MCP, click New MCP Server and use cursor-smartsh-mcp.json values.")
	fmt.Fprintln(out, "2) Paste agent-instructions.txt into system instructions.")
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
	if isHTTPReady(daemonURL+"/health", daemonToken, 1*time.Second) {
		return nil
	}
	if _, err := exec.LookPath("smartshd"); err == nil {
		if startError := startDetached("smartshd"); startError == nil {
			if waitHTTPReady(daemonURL+"/health", daemonToken, 6*time.Second) {
				return nil
			}
		}
	}
	rootDir := detectRootDir()
	if rootDir == "" {
		return fmt.Errorf("smartshd is not reachable and smartsh project root was not found")
	}
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("smartshd is not reachable and go command is missing for fallback start")
	}
	command := exec.Command("go", "run", filepath.Join(rootDir, "cmd/smartshd"))
	command.Dir = rootDir
	if err := startDetachedCommand(command); err != nil {
		return fmt.Errorf("failed to start smartshd: %w", err)
	}
	if !waitHTTPReady(daemonURL+"/health", daemonToken, 8*time.Second) {
		return fmt.Errorf("smartshd did not become healthy at %s", daemonURL)
	}
	return nil
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
		return "", "", "", nil, fmt.Errorf("could not locate smartsh root; set SMARTSH_ROOT and rerun setup-agent")
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

func startDetachedCommand(command *exec.Cmd) error {
	logFile, err := os.OpenFile(filepath.Join(os.TempDir(), "smartsh-setup.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	command.Stdout = logFile
	command.Stderr = logFile
	return command.Start()
}
