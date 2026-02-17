package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolveDaemonTokenPrefersConfig(t *testing.T) {
	t.Setenv("SMARTSH_DAEMON_TOKEN", "env-token")
	configValues := map[string]string{
		"SMARTSH_DAEMON_TOKEN": "config-token",
	}
	resolved := resolveDaemonToken(configValues)
	if resolved != "config-token" {
		t.Fatalf("expected config token, got %q", resolved)
	}
}

func TestToolsListIncludesMCPMaxWaitSec(t *testing.T) {
	server := &mcpServer{}
	response := server.handleRequest(rpcRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage("1"),
		Method:  "tools/list",
	})
	result, ok := response.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected tools/list result map")
	}
	tools, ok := result["tools"].([]map[string]interface{})
	if !ok || len(tools) == 0 {
		t.Fatalf("expected at least one tool in tools/list result")
	}
	schema, ok := tools[0]["inputSchema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inputSchema in tools/list result")
	}
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected inputSchema.properties in tools/list result")
	}
	if _, exists := properties["mcp_max_wait_sec"]; !exists {
		t.Fatalf("expected mcp_max_wait_sec property in tools/list schema")
	}
}

func TestCallSmartshRunReturnsCompletedJob(t *testing.T) {
	var runRequestBody map[string]interface{}
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewDecoder(request.Body).Decode(&runRequestBody)
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"status":           "completed",
				"executed":         true,
				"exit_code":        0,
				"summary":          "ok",
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	t.Setenv("SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL", "false")
	t.Setenv("SMARTSH_TERMINAL_APP", "terminal")

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshRun(map[string]interface{}{
		"command":                "go test ./...",
		"cwd":                    "/Applications/smartsh",
		"open_external_terminal": false,
	})
	if err != nil {
		t.Fatalf("callSmartshRun returned error: %v", err)
	}
	if response.Status != "completed" || response.ExitCode != 0 {
		t.Fatalf("expected completed response with zero exit code, got status=%q exit=%d", response.Status, response.ExitCode)
	}
	if runRequestBody["async"] != true {
		t.Fatalf("expected async run request")
	}
	if runRequestBody["open_external_terminal"] != false {
		t.Fatalf("expected open_external_terminal=false by default in MCP mode")
	}
	if _, exists := runRequestBody["terminal_app"]; exists {
		t.Fatalf("expected terminal_app to be omitted when open_external_terminal=false")
	}
	if runRequestBody["allowlist_mode"] != "warn" {
		t.Fatalf("expected default allowlist_mode warn, got %v", runRequestBody["allowlist_mode"])
	}
}

func TestCallSmartshRunCompactsDeterministicOutputTail(t *testing.T) {
	longTail := ""
	for index := 0; index < 1500; index++ {
		longTail += "x"
	}
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"status":           "failed",
				"executed":         true,
				"exit_code":        1,
				"summary_source":   "deterministic",
				"output_tail":      longTail,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshRun(map[string]interface{}{
		"command": "false",
		"cwd":     "/Applications/smartsh",
	})
	if err != nil {
		t.Fatalf("callSmartshRun returned error: %v", err)
	}
	if len(response.OutputTail) >= len(longTail) {
		t.Fatalf("expected deterministic output tail to be compacted")
	}
}

func TestCallSmartshApproveUsesApprovalEndpoint(t *testing.T) {
	var postedBody map[string]any
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/approvals/approval-xyz":
			_ = json.NewDecoder(request.Body).Decode(&postedBody)
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"status":           "completed",
				"executed":         true,
				"exit_code":        0,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshApprove(map[string]interface{}{
		"approval_id": "approval-xyz",
		"decision":    "yes",
	})
	if err != nil {
		t.Fatalf("callSmartshApprove returned error: %v", err)
	}
	if response.Status != "completed" || response.ExitCode != 0 {
		t.Fatalf("expected completed response, got status=%q exit=%d", response.Status, response.ExitCode)
	}
	if postedBody["approved"] != true {
		t.Fatalf("expected approved=true body, got %+v", postedBody)
	}
}

func TestCallSmartshRunReturnsLatestRunningWhenMaxWaitReached(t *testing.T) {
	var jobsPollCount int32
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"job_id":           "job-running",
				"status":           "running",
				"executed":         true,
				"exit_code":        0,
			})
		case "/jobs/job-running":
			atomic.AddInt32(&jobsPollCount, 1)
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"job_id":           "job-running",
				"status":           "running",
				"executed":         true,
				"exit_code":        0,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshRun(map[string]interface{}{
		"command":          "go test ./...",
		"cwd":              "/Applications/smartsh",
		"mcp_max_wait_sec": 1,
	})
	if err != nil {
		t.Fatalf("callSmartshRun returned error: %v", err)
	}
	if response.Status != "running" {
		t.Fatalf("expected running status after max wait, got %q", response.Status)
	}
	if response.Summary != "job still running; use job_id to poll status" {
		t.Fatalf("expected running summary hint, got %q", response.Summary)
	}
	if atomic.LoadInt32(&jobsPollCount) == 0 {
		t.Fatalf("expected at least one job poll")
	}
}

func TestCallSmartshRunPollsUntilCompletedWithinWait(t *testing.T) {
	var jobsPollCount int32
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"job_id":           "job-completes",
				"status":           "running",
				"executed":         true,
				"exit_code":        0,
			})
		case "/jobs/job-completes":
			current := atomic.AddInt32(&jobsPollCount, 1)
			if current < 2 {
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"must_use_smartsh": true,
					"job_id":           "job-completes",
					"status":           "running",
					"executed":         true,
					"exit_code":        0,
				})
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"job_id":           "job-completes",
				"status":           "completed",
				"executed":         true,
				"exit_code":        0,
				"summary":          "done",
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshRun(map[string]interface{}{
		"command":          "go test ./...",
		"cwd":              "/Applications/smartsh",
		"mcp_max_wait_sec": 3,
	})
	if err != nil {
		t.Fatalf("callSmartshRun returned error: %v", err)
	}
	if response.Status != "completed" || response.ExitCode != 0 {
		t.Fatalf("expected completed response, got status=%q exit=%d", response.Status, response.ExitCode)
	}
	if atomic.LoadInt32(&jobsPollCount) < 2 {
		t.Fatalf("expected multiple job polls before completion")
	}
}

func TestCallSmartshRunNeedsApprovalPrompt(t *testing.T) {
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh":  true,
				"job_id":            "job-needs-approval",
				"status":            "needs_approval",
				"executed":          false,
				"exit_code":         0,
				"approval_id":       "approval-123",
				"risk_targets":      []string{"/tmp/node_modules", "/tmp/kpi-overview"},
				"requires_approval": true,
				"approval_message":  "approval required",
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	response, err := server.callSmartshRun(map[string]interface{}{
		"command": "rm -rf node_modules",
		"cwd":     "/Applications/smartsh",
	})
	if err != nil {
		t.Fatalf("callSmartshRun returned error: %v", err)
	}
	if response.Status != "needs_approval" {
		t.Fatalf("expected needs_approval status, got %q", response.Status)
	}
	if response.ApprovalID != "approval-123" {
		t.Fatalf("expected approval id to be preserved, got %q", response.ApprovalID)
	}
	if response.ApprovalHowTo == "" {
		t.Fatalf("expected approval_howto to be populated")
	}
	if response.Summary == "" || response.Summary == "approval required" {
		t.Fatalf("expected summary to include approval prompt, got %q", response.Summary)
	}
}

func TestCallSmartshRunApprovalYesShortcutUsesLastApprovalID(t *testing.T) {
	var approvalPosted bool
	mockDaemon := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/health":
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(`{"ok":true}`))
		case "/run":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"status":           "needs_approval",
				"executed":         false,
				"exit_code":        0,
				"approval_id":      "approval-yes-shortcut",
				"risk_targets":     []string{"/tmp/node_modules"},
			})
		case "/approvals/approval-yes-shortcut":
			approvalPosted = true
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"must_use_smartsh": true,
				"status":           "completed",
				"executed":         true,
				"exit_code":        0,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer mockDaemon.Close()

	server := &mcpServer{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		daemonURL:  mockDaemon.URL,
	}
	_, err := server.callSmartshRun(map[string]interface{}{
		"command": "rm -rf node_modules",
		"cwd":     "/Applications/smartsh",
	})
	if err != nil {
		t.Fatalf("first callSmartshRun returned error: %v", err)
	}

	approvedResponse, approveError := server.callSmartshRun(map[string]interface{}{
		"approval_response": "y",
	})
	if approveError != nil {
		t.Fatalf("approval shortcut returned error: %v", approveError)
	}
	if !approvalPosted {
		t.Fatalf("expected approval endpoint to be called")
	}
	if approvedResponse.Status != "completed" || approvedResponse.ExitCode != 0 {
		t.Fatalf("expected completed approval response, got status=%q exit=%d", approvedResponse.Status, approvedResponse.ExitCode)
	}
}
