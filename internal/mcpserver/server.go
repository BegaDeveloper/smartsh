package mcpserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BegaDeveloper/smartsh/internal/runtimeconfig"
)

const (
	defaultRunTimeoutSec         = 180
	defaultMCPMaxWaitSec         = 25
	defaultMCPMaxOutputTailChars = 600
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type daemonRunResponse struct {
	MustUseSmartsh   bool     `json:"must_use_smartsh"`
	JobID            string   `json:"job_id,omitempty"`
	Status           string   `json:"status,omitempty"`
	Executed         bool     `json:"executed"`
	ResolvedCommand  string   `json:"resolved_command,omitempty"`
	ExitCode         int      `json:"exit_code"`
	Summary          string   `json:"summary,omitempty"`
	SummarySource    string   `json:"summary_source,omitempty"`
	ErrorType        string   `json:"error_type,omitempty"`
	PrimaryError     string   `json:"primary_error,omitempty"`
	NextAction       string   `json:"next_action,omitempty"`
	FailingTests     []string `json:"failing_tests,omitempty"`
	FailedFiles      []string `json:"failed_files,omitempty"`
	TopIssues        []string `json:"top_issues,omitempty"`
	BlockedReason    string   `json:"blocked_reason,omitempty"`
	RequiresApproval bool     `json:"requires_approval,omitempty"`
	ApprovalID       string   `json:"approval_id,omitempty"`
	ApprovalMessage  string   `json:"approval_message,omitempty"`
	ApprovalHowTo    string   `json:"approval_howto,omitempty"`
	RiskReason       string   `json:"risk_reason,omitempty"`
	RiskTargets      []string `json:"risk_targets,omitempty"`
	Error            string   `json:"error,omitempty"`
	DurationMS       int64    `json:"duration_ms,omitempty"`
	OutputTail       string   `json:"output_tail,omitempty"`
}

type mcpServer struct {
	reader         *bufio.Reader
	writer         *bufio.Writer
	writeMutex     sync.Mutex
	stateMutex     sync.Mutex
	httpClient     *http.Client
	daemonURL      string
	daemonToken    string
	initialized    bool
	useLineJSON    bool
	lastApprovalID string
}

func Run() error {
	configValues := map[string]string{}
	config, configErr := runtimeconfig.Load("")
	if configErr == nil {
		configValues = config.Values
	}
	server := &mcpServer{
		reader:      bufio.NewReader(os.Stdin),
		writer:      bufio.NewWriter(os.Stdout),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		daemonURL:   daemonURLFromEnv(),
		daemonToken: resolveDaemonToken(configValues),
	}
	return server.loop()
}

func (server *mcpServer) loop() error {
	for {
		requestBytes, isLineJSON, err := readRPCMessage(server.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		server.useLineJSON = isLineJSON
		request := rpcRequest{}
		if err := json.Unmarshal(requestBytes, &request); err != nil {
			_ = server.writeResponse(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}
		if request.Method == "" {
			continue
		}
		if request.Method == "notifications/initialized" {
			server.initialized = true
			continue
		}
		if request.Method == "exit" {
			return nil
		}
		response := server.handleRequest(request)
		if len(request.ID) == 0 {
			continue
		}
		if err := server.writeResponse(response); err != nil {
			return err
		}
	}
}

func (server *mcpServer) handleRequest(request rpcRequest) rpcResponse {
	response := rpcResponse{
		JSONRPC: "2.0",
		ID:      decodeID(request.ID),
	}

	switch request.Method {
	case "initialize":
		requestedProtocolVersion := "2024-11-05"
		initParams := initializeParams{}
		if err := json.Unmarshal(request.Params, &initParams); err == nil {
			if strings.TrimSpace(initParams.ProtocolVersion) != "" {
				requestedProtocolVersion = strings.TrimSpace(initParams.ProtocolVersion)
			}
		}
		response.Result = map[string]interface{}{
			"protocolVersion": requestedProtocolVersion,
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": false,
				},
			},
			"serverInfo": map[string]string{
				"name":    "smartsh-mcp",
				"version": "1.0.0",
			},
		}
		return response
	case "ping":
		response.Result = map[string]interface{}{}
		return response
	case "tools/list":
		response.Result = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "smartsh_run",
					"description": "Run terminal command through local smartshd and return compact summary JSON.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"command":                map[string]string{"type": "string"},
							"cwd":                    map[string]string{"type": "string"},
							"dry_run":                map[string]string{"type": "boolean"},
							"unsafe":                 map[string]string{"type": "boolean"},
							"require_approval":       map[string]string{"type": "boolean"},
							"timeout_sec":            map[string]string{"type": "integer"},
							"mcp_max_wait_sec":       map[string]string{"type": "integer"},
							"allowlist_mode":         map[string]interface{}{"type": "string", "enum": []string{"off", "warn", "enforce"}},
							"allowlist_file":         map[string]string{"type": "string"},
							"open_external_terminal": map[string]string{"type": "boolean"},
							"terminal_app":           map[string]string{"type": "string"},
							"terminal_session_key":   map[string]string{"type": "string"},
							"approval_id":            map[string]string{"type": "string"},
							"approval_response":      map[string]string{"type": "string"},
						},
					},
				},
				{
					"name":        "smartsh_approve",
					"description": "Approve or reject a pending risky smartsh command by approval_id. Use decision=y|yes|n|no.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"approval_id":      map[string]string{"type": "string"},
							"decision":         map[string]string{"type": "string"},
							"approved":         map[string]string{"type": "boolean"},
							"mcp_max_wait_sec": map[string]string{"type": "integer"},
						},
					},
				},
			},
		}
		return response
	case "tools/call":
		params := toolCallParams{}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &rpcError{Code: -32602, Message: "invalid tool call params"}
			return response
		}
		if params.Name != "smartsh_run" && params.Name != "smartsh_approve" {
			response.Error = &rpcError{Code: -32601, Message: "unknown tool"}
			return response
		}
		var runResult daemonRunResponse
		var callErr error
		if params.Name == "smartsh_run" {
			runResult, callErr = server.callSmartshRun(params.Arguments)
		} else {
			runResult, callErr = server.callSmartshApprove(params.Arguments)
		}
		if callErr != nil {
			response.Result = map[string]interface{}{
				"isError": true,
				"content": []map[string]string{
					{"type": "text", "text": fmt.Sprintf(`{"executed":false,"exit_code":1,"error":"%s"}`, sanitizeError(callErr))},
				},
			}
			return response
		}
		resultJSON, _ := json.Marshal(runResult)
		response.Result = map[string]interface{}{
			"content": []map[string]string{
				{"type": "text", "text": string(resultJSON)},
			},
			"structuredContent": runResult,
			"isError":           runResult.ExitCode != 0,
		}
		return response
	default:
		response.Error = &rpcError{Code: -32601, Message: "method not found"}
		return response
	}
}

func (server *mcpServer) callSmartshRun(arguments map[string]interface{}) (daemonRunResponse, error) {
	if err := server.ensureDaemon(); err != nil {
		return daemonRunResponse{}, err
	}
	if handledResponse, handled, handleError := server.handleApprovalShortcut(arguments); handled {
		server.compactRunResponse(&handledResponse)
		return handledResponse, handleError
	}

	requestBody := map[string]interface{}{
		"async": true,
	}
	for _, key := range []string{"command", "cwd", "dry_run", "unsafe", "require_approval", "allowlist_mode", "allowlist_file", "open_external_terminal", "terminal_app", "terminal_session_key"} {
		if value, exists := arguments[key]; exists {
			requestBody[key] = value
		}
	}
	if _, exists := requestBody["allowlist_mode"]; !exists {
		requestBody["allowlist_mode"] = mcpDefaultAllowlistMode()
	}
	if _, exists := requestBody["require_approval"]; !exists {
		requestBody["require_approval"] = true
	}
	if _, exists := requestBody["open_external_terminal"]; !exists {
		requestBody["open_external_terminal"] = mcpOpenExternalTerminalEnabled()
	}
	openExternalTerminal, _ := requestBody["open_external_terminal"].(bool)
	if openExternalTerminal {
		if _, exists := requestBody["terminal_app"]; !exists {
			if terminalApp := strings.TrimSpace(os.Getenv("SMARTSH_TERMINAL_APP")); terminalApp != "" {
				requestBody["terminal_app"] = terminalApp
			}
		}
	}
	if _, exists := requestBody["terminal_session_key"]; !exists {
		requestBody["terminal_session_key"] = "cursor-main"
	}
	timeoutSec := toInt(arguments["timeout_sec"])
	if timeoutSec <= 0 {
		timeoutSec = defaultRunTimeoutSec
	}
	requestBody["timeout_sec"] = timeoutSec
	maxWaitSec := toInt(arguments["mcp_max_wait_sec"])
	if maxWaitSec <= 0 {
		maxWaitSec = defaultMCPMaxWaitSec
	}

	initial, err := server.postRun(requestBody)
	if err != nil {
		return daemonRunResponse{}, err
	}
	return server.waitForJobIfNeeded(initial, maxWaitSec)
}

func (server *mcpServer) callSmartshApprove(arguments map[string]interface{}) (daemonRunResponse, error) {
	if err := server.ensureDaemon(); err != nil {
		return daemonRunResponse{}, err
	}
	maxWaitSec := toInt(arguments["mcp_max_wait_sec"])
	if maxWaitSec <= 0 {
		maxWaitSec = defaultMCPMaxWaitSec
	}
	approvalID := strings.TrimSpace(toString(arguments["approval_id"]))
	if approvalID == "" {
		server.stateMutex.Lock()
		approvalID = server.lastApprovalID
		server.stateMutex.Unlock()
	}
	if approvalID == "" {
		return daemonRunResponse{}, fmt.Errorf("approval_id is required")
	}
	approved, decisionError := parseApprovalDecision(arguments)
	if decisionError != nil {
		return daemonRunResponse{}, decisionError
	}
	initial, err := server.postApproval(approvalID, approved)
	if err != nil {
		return daemonRunResponse{}, err
	}
	return server.waitForJobIfNeeded(initial, maxWaitSec)
}

func (server *mcpServer) waitForJobIfNeeded(initial daemonRunResponse, maxWaitSec int) (daemonRunResponse, error) {
	if initial.JobID == "" || isTerminalJobStatus(initial.Status) {
		server.decorateApprovalPrompt(&initial)
		server.compactRunResponse(&initial)
		return initial, nil
	}

	deadline := time.Now().Add(time.Duration(maxWaitSec) * time.Second)
	lastKnown := initial
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		job, pollErr := server.getJob(initial.JobID)
		if pollErr != nil {
			return daemonRunResponse{}, pollErr
		}
		lastKnown = job
		if isTerminalJobStatus(job.Status) {
			server.decorateApprovalPrompt(&job)
			server.compactRunResponse(&job)
			return job, nil
		}
	}
	if strings.TrimSpace(lastKnown.Summary) == "" ||
		strings.EqualFold(strings.TrimSpace(lastKnown.Summary), "job accepted") ||
		strings.EqualFold(strings.TrimSpace(lastKnown.Summary), "job running") {
		lastKnown.Summary = "job still running; use job_id to poll status"
	}
	server.decorateApprovalPrompt(&lastKnown)
	server.compactRunResponse(&lastKnown)
	return lastKnown, nil
}

func (server *mcpServer) handleApprovalShortcut(arguments map[string]interface{}) (daemonRunResponse, bool, error) {
	approvalID := strings.TrimSpace(toString(arguments["approval_id"]))
	approvalResponse := strings.TrimSpace(strings.ToLower(toString(arguments["approval_response"])))
	if approvalResponse == "" {
		return daemonRunResponse{}, false, nil
	}
	if approvalID == "" {
		server.stateMutex.Lock()
		approvalID = server.lastApprovalID
		server.stateMutex.Unlock()
	}
	if approvalID == "" {
		return daemonRunResponse{}, true, fmt.Errorf("approval_id is required for approval responses")
	}
	approved := approvalResponse == "y" || approvalResponse == "yes"
	response, err := server.postApproval(approvalID, approved)
	if err != nil {
		return daemonRunResponse{}, true, err
	}
	if approved {
		server.stateMutex.Lock()
		server.lastApprovalID = ""
		server.stateMutex.Unlock()
	}
	server.decorateApprovalPrompt(&response)
	return response, true, nil
}

func (server *mcpServer) postApproval(approvalID string, approved bool) (daemonRunResponse, error) {
	requestBytes, err := json.Marshal(map[string]bool{"approved": approved})
	if err != nil {
		return daemonRunResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, server.daemonURL+"/approvals/"+approvalID, bytes.NewReader(requestBytes))
	if err != nil {
		return daemonRunResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	server.applyAuthHeaders(request)

	response, err := server.httpClient.Do(request)
	if err != nil {
		return daemonRunResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return daemonRunResponse{}, err
	}
	runResponse := daemonRunResponse{}
	if err := json.Unmarshal(body, &runResponse); err != nil {
		return daemonRunResponse{}, err
	}
	if response.StatusCode >= 400 && runResponse.Error != "" {
		return daemonRunResponse{}, fmt.Errorf(runResponse.Error)
	}
	return runResponse, nil
}

func (server *mcpServer) decorateApprovalPrompt(response *daemonRunResponse) {
	if response == nil {
		return
	}
	if strings.ToLower(strings.TrimSpace(response.Status)) != "needs_approval" || strings.TrimSpace(response.ApprovalID) == "" {
		return
	}
	server.stateMutex.Lock()
	server.lastApprovalID = response.ApprovalID
	server.stateMutex.Unlock()

	targetsText := "critical resources"
	if len(response.RiskTargets) > 0 {
		targetsText = strings.Join(response.RiskTargets, ", ")
	}
	prompt := "You are about to modify: " + targetsText + ". Approve? (y/n) using approval_id=" + response.ApprovalID
	response.ApprovalHowTo = fmt.Sprintf(`Use smartsh_approve with {"approval_id":"%s","decision":"yes"} to approve or {"approval_id":"%s","decision":"no"} to reject.`, response.ApprovalID, response.ApprovalID)
	if strings.TrimSpace(response.Summary) == "" {
		response.Summary = prompt
	} else if !strings.Contains(response.Summary, "Approve? (y/n)") {
		response.Summary = response.Summary + " " + prompt
	}
}

func (server *mcpServer) compactRunResponse(response *daemonRunResponse) {
	if response == nil || !mcpCompactOutputEnabled() {
		return
	}
	maxChars := mcpMaxOutputTailChars()
	if maxChars <= 0 || len(response.OutputTail) <= maxChars {
		return
	}
	response.OutputTail = response.OutputTail[len(response.OutputTail)-maxChars:] + "\n[truncated by smartsh mcp compact mode]\n"
}

func (server *mcpServer) postRun(requestBody map[string]interface{}) (daemonRunResponse, error) {
	requestBytes, err := json.Marshal(requestBody)
	if err != nil {
		return daemonRunResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, server.daemonURL+"/run", bytes.NewReader(requestBytes))
	if err != nil {
		return daemonRunResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	server.applyAuthHeaders(request)

	response, err := server.httpClient.Do(request)
	if err != nil {
		return daemonRunResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return daemonRunResponse{}, err
	}
	runResponse := daemonRunResponse{}
	if err := json.Unmarshal(body, &runResponse); err != nil {
		return daemonRunResponse{}, err
	}
	if runResponse.Error != "" && runResponse.JobID == "" && runResponse.ExitCode != 0 && response.StatusCode >= 400 {
		return runResponse, fmt.Errorf(runResponse.Error)
	}
	return runResponse, nil
}

func (server *mcpServer) getJob(jobID string) (daemonRunResponse, error) {
	request, err := http.NewRequest(http.MethodGet, server.daemonURL+"/jobs/"+jobID, nil)
	if err != nil {
		return daemonRunResponse{}, err
	}
	server.applyAuthHeaders(request)
	response, err := server.httpClient.Do(request)
	if err != nil {
		return daemonRunResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return daemonRunResponse{}, err
	}
	job := daemonRunResponse{}
	if err := json.Unmarshal(body, &job); err != nil {
		return daemonRunResponse{}, err
	}
	if response.StatusCode >= 400 && job.Error != "" {
		return daemonRunResponse{}, fmt.Errorf(job.Error)
	}
	return job, nil
}

func (server *mcpServer) ensureDaemon() error {
	if server.isDaemonHealthy() {
		return nil
	}
	for _, command := range daemonStartCandidates() {
		command.Env = append(os.Environ(),
			"SMARTSH_DAEMON_TOKEN="+server.daemonToken,
			"SMARTSH_DAEMON_DISABLE_AUTH=false",
		)
		if startErr := startDetachedProcess(command); startErr == nil && server.waitDaemonHealthy(12*time.Second) {
			return nil
		}
	}
	rootDir := detectRootDir()
	if rootDir == "" {
		return fmt.Errorf("smartshd is not reachable and smartsh root was not found")
	}
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("smartshd is not reachable and go command is unavailable")
	}
	command := exec.Command("go", "run", filepath.Join(rootDir, "cmd/smartshd"))
	command.Dir = rootDir
	command.Env = append(os.Environ(),
		"SMARTSH_DAEMON_TOKEN="+server.daemonToken,
		"SMARTSH_DAEMON_DISABLE_AUTH=false",
	)
	if err := startDetachedProcess(command); err != nil {
		return fmt.Errorf("failed to start smartshd: %w", err)
	}
	if !server.waitDaemonHealthy(10 * time.Second) {
		return fmt.Errorf("smartshd did not become healthy at %s", server.daemonURL)
	}
	return nil
}

func (server *mcpServer) isDaemonHealthy() bool {
	request, err := http.NewRequest(http.MethodGet, server.daemonURL+"/health", nil)
	if err != nil {
		return false
	}
	server.applyAuthHeaders(request)
	response, err := server.httpClient.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (server *mcpServer) waitDaemonHealthy(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if server.isDaemonHealthy() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (server *mcpServer) applyAuthHeaders(request *http.Request) {
	if strings.TrimSpace(server.daemonToken) == "" {
		return
	}
	request.Header.Set("X-Smartsh-Token", server.daemonToken)
}

func daemonURLFromEnv() string {
	url := strings.TrimSpace(os.Getenv("SMARTSH_DAEMON_URL"))
	if url == "" {
		return "http://127.0.0.1:8787"
	}
	return url
}

func resolveDaemonToken(configValues map[string]string) string {
	// Prefer ~/.smartsh/config to avoid stale per-project MCP env tokens.
	if configValues != nil {
		if token := strings.TrimSpace(configValues["SMARTSH_DAEMON_TOKEN"]); token != "" {
			return token
		}
	}
	return strings.TrimSpace(os.Getenv("SMARTSH_DAEMON_TOKEN"))
}

func detectRootDir() string {
	if envRoot := strings.TrimSpace(os.Getenv("SMARTSH_ROOT")); envRoot != "" {
		if hasSmartshSourceLayout(envRoot) {
			return envRoot
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if resolved := walkForRoot(cwd); resolved != "" {
			return resolved
		}
	}
	if executablePath, err := os.Executable(); err == nil {
		if resolved := walkForRoot(filepath.Dir(executablePath)); resolved != "" {
			return resolved
		}
	}
	if hasSmartshSourceLayout("/Applications/smartsh") {
		return "/Applications/smartsh"
	}
	return ""
}

func walkForRoot(start string) string {
	current := start
	for {
		if hasSmartshSourceLayout(current) {
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

func hasSmartshSourceLayout(path string) bool {
	_, modErr := os.Stat(filepath.Join(path, "go.mod"))
	_, cmdErr := os.Stat(filepath.Join(path, "cmd", "smartshd"))
	return modErr == nil && cmdErr == nil
}

func startDetachedProcess(command *exec.Cmd) error {
	logPath := filepath.Join(os.TempDir(), "smartsh-mcp.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	command.Stdout = logFile
	command.Stderr = logFile
	if runtime.GOOS == "windows" {
		return command.Start()
	}
	return command.Start()
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

func readRPCMessage(reader *bufio.Reader) ([]byte, bool, error) {
	// Some MCP clients send JSON-RPC as newline-delimited JSON over stdio,
	// while others use Content-Length framing.
	for {
		peeked, err := reader.Peek(1)
		if err != nil {
			return nil, false, err
		}
		if len(peeked) == 0 {
			return nil, false, io.EOF
		}
		switch peeked[0] {
		case ' ', '\t', '\r', '\n':
			if _, err := reader.ReadByte(); err != nil {
				return nil, false, err
			}
			continue
		case '{', '[':
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					trimmed := bytes.TrimSpace(line)
					if len(trimmed) > 0 {
						return trimmed, true, nil
					}
				}
				return nil, false, err
			}
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) == 0 {
				continue
			}
			return trimmed, true, nil
		default:
			payload, readErr := readFramedMessage(reader)
			return payload, false, readErr
		}
	}
}

func readFramedMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "" {
			break
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "content-length:") {
			rawLength := strings.TrimSpace(trimmed[len("content-length:"):])
			parsedLength, parseErr := strconv.Atoi(rawLength)
			if parseErr != nil {
				return nil, parseErr
			}
			contentLength = parsedLength
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}
	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (server *mcpServer) writeResponse(response rpcResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	server.writeMutex.Lock()
	defer server.writeMutex.Unlock()
	if server.useLineJSON {
		if _, err := server.writer.Write(payload); err != nil {
			return err
		}
		if err := server.writer.WriteByte('\n'); err != nil {
			return err
		}
		return server.writer.Flush()
	}
	if _, err := fmt.Fprintf(server.writer, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := server.writer.Write(payload); err != nil {
		return err
	}
	return server.writer.Flush()
}

func decodeID(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var integerID int64
	if err := json.Unmarshal(raw, &integerID); err == nil {
		return integerID
	}
	var stringID string
	if err := json.Unmarshal(raw, &stringID); err == nil {
		return stringID
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err == nil {
		return generic
	}
	return nil
}

func isTerminalJobStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "blocked", "needs_approval":
		return true
	default:
		return false
	}
}

func toInt(value interface{}) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func toString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func parseApprovalDecision(arguments map[string]interface{}) (bool, error) {
	if approvedValue, exists := arguments["approved"]; exists {
		switch typed := approvedValue.(type) {
		case bool:
			return typed, nil
		case string:
			normalized := strings.ToLower(strings.TrimSpace(typed))
			if normalized == "true" {
				return true, nil
			}
			if normalized == "false" {
				return false, nil
			}
		}
	}
	decision := strings.ToLower(strings.TrimSpace(toString(arguments["decision"])))
	switch decision {
	case "y", "yes", "approve", "approved":
		return true, nil
	case "n", "no", "reject", "rejected":
		return false, nil
	case "":
		return false, fmt.Errorf("decision is required: y/yes or n/no")
	default:
		return false, fmt.Errorf("invalid decision %q, expected y/yes or n/no", decision)
	}
}

func sanitizeError(err error) string {
	message := strings.ReplaceAll(err.Error(), `"`, `'`)
	message = strings.ReplaceAll(message, "\n", " ")
	if strings.TrimSpace(message) == "" {
		return "unknown error"
	}
	return message
}

func parseEnvBool(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func mcpCompactOutputEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTSH_MCP_COMPACT_OUTPUT")))
	switch raw {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func mcpMaxOutputTailChars() int {
	raw := strings.TrimSpace(os.Getenv("SMARTSH_MCP_MAX_OUTPUT_TAIL_CHARS"))
	if raw == "" {
		return defaultMCPMaxOutputTailChars
	}
	parsed, parseError := strconv.Atoi(raw)
	if parseError != nil {
		return defaultMCPMaxOutputTailChars
	}
	return parsed
}

func mcpOpenExternalTerminalEnabled() bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL")))
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off", "":
		return false
	default:
		return false
	}
}

func mcpDefaultAllowlistMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE")))
	switch mode {
	case "off", "warn", "enforce":
		return mode
	default:
		return "warn"
	}
}
