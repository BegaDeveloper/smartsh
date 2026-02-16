package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeterministicSummary_Jest(t *testing.T) {
	output := readFixture(t, "jest_fail.log")
	result := deterministicSummary("npm test", 1, output, nil)
	if result.ErrorType != "test" {
		t.Fatalf("expected test error type, got %q", result.ErrorType)
	}
	if len(result.FailingTests) == 0 {
		t.Fatalf("expected failing tests to be parsed")
	}
	if len(result.FailedFiles) == 0 {
		t.Fatalf("expected failed files to be parsed")
	}
}

func TestDeterministicSummary_GoTest(t *testing.T) {
	output := readFixture(t, "go_test_fail.log")
	result := deterministicSummary("go test ./...", 1, output, nil)
	if result.ErrorType != "test" {
		t.Fatalf("expected test error type, got %q", result.ErrorType)
	}
	if len(result.FailingTests) == 0 {
		t.Fatalf("expected failing tests to be parsed")
	}
}

func TestDeterministicSummary_TypeScript(t *testing.T) {
	output := readFixture(t, "tsc_fail.log")
	result := deterministicSummary("npm run build", 1, output, nil)
	if result.ErrorType != "compile" {
		t.Fatalf("expected compile error type, got %q", result.ErrorType)
	}
	if len(result.FailedFiles) == 0 {
		t.Fatalf("expected failed files to be parsed")
	}
}

func TestJobStorePersistence(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "smartshd.db")

	store, err := newJobStore(dbPath)
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	job := daemonJob{
		ID: "job_test_1",
		Result: runResponse{
			MustUseSmartsh: true,
			Status:         "completed",
			Executed:       true,
			ExitCode:       0,
			Summary:        "ok",
		},
	}
	if err := store.Save(job); err != nil {
		t.Fatalf("save failed: %v", err)
	}
	_ = store.Close()

	store2, err := newJobStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store failed: %v", err)
	}
	defer store2.Close()

	reloaded, err := store2.Get("job_test_1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if reloaded == nil || reloaded.Result.Summary != "ok" {
		t.Fatalf("expected persisted job, got %+v", reloaded)
	}
}

func TestExecuteRequest_SuccessOmitsOutputTail(t *testing.T) {
	t.Setenv("SMARTSH_SUMMARY_PROVIDER", "deterministic")
	tempDir := t.TempDir()
	store, err := newJobStore(filepath.Join(tempDir, "jobs.db"))
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer store.Close()

	server := newDaemonServer(store)
	response := server.executeRequest(context.Background(), runRequest{
		Command: "echo smartsh",
		Cwd:     tempDir,
		Unsafe:  true,
	}, "")

	if response.Status != "completed" {
		t.Fatalf("expected completed status, got %q", response.Status)
	}
	if response.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", response.ExitCode)
	}
	if response.OutputTail != "" {
		t.Fatalf("expected empty output tail for successful run, got %q", response.OutputTail)
	}
	if response.SummarySource != "deterministic" {
		t.Fatalf("expected deterministic summary source for successful run, got %q", response.SummarySource)
	}
}

func TestExecuteRequest_FailureIncludesOutputTail(t *testing.T) {
	tempDir := t.TempDir()
	store, err := newJobStore(filepath.Join(tempDir, "jobs.db"))
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer store.Close()

	server := newDaemonServer(store)
	response := server.executeRequest(context.Background(), runRequest{
		Command: "echo smartsh-error && false",
		Cwd:     tempDir,
		Unsafe:  true,
	}, "")

	if response.Status != "failed" {
		t.Fatalf("expected failed status, got %q", response.Status)
	}
	if response.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code, got %d", response.ExitCode)
	}
	if response.OutputTail == "" {
		t.Fatalf("expected output tail for failed run")
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return string(data)
}

func TestExecuteRequest_RiskyCommandNeedsApproval(t *testing.T) {
	tempDir := t.TempDir()
	store, err := newJobStore(filepath.Join(tempDir, "jobs.db"))
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer store.Close()

	server := newDaemonServer(store)
	response := server.executeRequest(context.Background(), runRequest{
		Command:         "rm -rf ./build",
		Cwd:             tempDir,
		RequireApproval: true,
		Unsafe:          false,
	}, "")

	if response.Status != "needs_approval" {
		t.Fatalf("expected needs_approval status, got %q", response.Status)
	}
	if !strings.Contains(response.ApprovalHowTo, "smartsh_approve") {
		t.Fatalf("expected approval_howto guidance, got %q", response.ApprovalHowTo)
	}
	if response.ApprovalID == "" {
		t.Fatalf("expected approval id in response")
	}
	if len(response.RiskTargets) == 0 {
		t.Fatalf("expected at least one risk target")
	}
	approval, approvalError := store.GetApproval(response.ApprovalID)
	if approvalError != nil {
		t.Fatalf("get approval failed: %v", approvalError)
	}
	if approval == nil {
		t.Fatalf("expected approval to be persisted")
	}
	if approval.ResolvedRisk != "high" {
		t.Fatalf("expected resolved risk high, got %q", approval.ResolvedRisk)
	}
}

func TestHandleApprovalRoutes_RejectsPendingApproval(t *testing.T) {
	t.Setenv("SMARTSH_DAEMON_DISABLE_AUTH", "true")
	tempDir := t.TempDir()
	store, err := newJobStore(filepath.Join(tempDir, "jobs.db"))
	if err != nil {
		t.Fatalf("open store failed: %v", err)
	}
	defer store.Close()

	server := newDaemonServer(store)
	approval := commandApproval{
		ID: "approval_test_reject",
		Request: runRequest{
			Command:         "rm -rf ./build",
			Cwd:             tempDir,
			RequireApproval: true,
		},
		ResolvedCommand: "rm -rf ./build",
		ResolvedRisk:    "high",
		RiskReason:      "recursive delete",
		Status:          "pending",
	}
	if saveError := store.SaveApproval(approval); saveError != nil {
		t.Fatalf("save approval failed: %v", saveError)
	}

	request := httptest.NewRequest(http.MethodPost, "/approvals/"+approval.ID, strings.NewReader(`{"approved":false}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.handleApprovalRoutes(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	body, readError := io.ReadAll(recorder.Body)
	if readError != nil {
		t.Fatalf("read response body failed: %v", readError)
	}
	response := runResponse{}
	if unmarshalError := json.Unmarshal(body, &response); unmarshalError != nil {
		t.Fatalf("parse response failed: %v", unmarshalError)
	}
	if response.Status != "blocked" {
		t.Fatalf("expected blocked status, got %q", response.Status)
	}
	updatedApproval, getError := store.GetApproval(approval.ID)
	if getError != nil {
		t.Fatalf("get updated approval failed: %v", getError)
	}
	if updatedApproval == nil || updatedApproval.Status != "rejected" {
		t.Fatalf("expected rejected approval status, got %+v", updatedApproval)
	}
}
