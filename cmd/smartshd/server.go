package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/BegaDeveloper/smartsh/internal/security"
)

const (
	defaultRunMaxOutputKB      = 48
	failedRunOutputTailMaxSize = 1200
)

type daemonServer struct {
	cwdMutex         sync.Mutex
	store            *jobStore
	httpClient       *http.Client
	metrics          *metricsRegistry
	authDisabled     bool
	daemonToken      string
	subscribersMutex sync.Mutex
	subscribers      map[string]map[chan runResponse]struct{}
	ptySessionsMutex sync.Mutex
	ptySessions      map[string]*ptySession
}

func newDaemonServer(store *jobStore) *daemonServer {
	authDisabled, daemonToken := resolveDaemonAuthConfig()
	return &daemonServer{
		store:        store,
		httpClient:   &http.Client{Timeout: 25 * time.Second},
		metrics:      newMetricsRegistry(),
		authDisabled: authDisabled,
		daemonToken:  daemonToken,
		subscribers:  map[string]map[chan runResponse]struct{}{},
		ptySessions:  map[string]*ptySession{},
	}
}

func (server *daemonServer) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		return
	}
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true, "service": "smartshd", "must_use_smartsh": true})
}

func (server *daemonServer) handleRun(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "method not allowed"})
		return
	}
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "unauthorized"})
		return
	}

	runRequestPayload := runRequest{}
	if decodeError := json.NewDecoder(request.Body).Decode(&runRequestPayload); decodeError != nil {
		writeJSON(writer, http.StatusBadRequest, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: fmt.Sprintf("invalid request body: %v", decodeError)})
		return
	}

	if runRequestPayload.Async {
		job := daemonJob{
			ID:        fmt.Sprintf("job_%d", time.Now().UnixNano()),
			Request:   runRequestPayload,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Result: runResponse{
				MustUseSmartsh: true,
				JobID:          "",
				Status:         "queued",
				Executed:       false,
				ExitCode:       0,
				Summary:        "job accepted",
			},
		}
		job.Result.JobID = job.ID
		_ = server.store.Save(job)
		go server.executeJob(job.ID)
		writeJSON(writer, http.StatusAccepted, job.Result)
		return
	}

	runResponsePayload := server.executeRequest(request.Context(), runRequestPayload, "")
	server.metrics.recordRun(runResponsePayload)
	statusCode := http.StatusOK
	if runResponsePayload.Error != "" && runResponsePayload.ExitCode != 0 {
		statusCode = http.StatusBadRequest
	}
	writeJSON(writer, statusCode, runResponsePayload)
}

func (server *daemonServer) handleJobs(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "method not allowed"})
		return
	}
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "unauthorized"})
		return
	}
	limit := 50
	if rawLimit := strings.TrimSpace(request.URL.Query().Get("limit")); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil {
			limit = parsed
		}
	}
	jobs, err := server.store.List(limit)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"must_use_smartsh": true, "error": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"must_use_smartsh": true, "jobs": jobs})
}

func (server *daemonServer) handleJobRoutes(writer http.ResponseWriter, request *http.Request) {
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "unauthorized"})
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/jobs/")
	path = strings.TrimSpace(path)
	if path == "" {
		writeJSON(writer, http.StatusBadRequest, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "job id is required"})
		return
	}
	if strings.HasSuffix(path, "/stream") {
		jobID := strings.TrimSuffix(path, "/stream")
		jobID = strings.TrimSuffix(jobID, "/")
		server.handleJobStream(writer, request, jobID)
		return
	}
	server.handleJobByID(writer, request, path)
}

func (server *daemonServer) handleApprovalRoutes(writer http.ResponseWriter, request *http.Request) {
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "unauthorized"})
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/approvals/")
	approvalID := strings.TrimSpace(strings.TrimSuffix(path, "/"))
	if approvalID == "" {
		writeJSON(writer, http.StatusBadRequest, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "approval id is required"})
		return
	}

	approval, approvalError := server.store.GetApproval(approvalID)
	if approvalError != nil {
		writeJSON(writer, http.StatusInternalServerError, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: approvalError.Error()})
		return
	}
	if approval == nil {
		writeJSON(writer, http.StatusNotFound, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "approval not found"})
		return
	}

	switch request.Method {
	case http.MethodGet:
		writeJSON(writer, http.StatusOK, map[string]any{
			"must_use_smartsh": true,
			"approval_id":      approval.ID,
			"status":           approval.Status,
			"job_id":           approval.JobID,
			"resolved_command": approval.ResolvedCommand,
			"risk_reason":      approval.RiskReason,
			"risk_targets":     approval.RiskTargets,
			"created_at":       approval.CreatedAt,
			"updated_at":       approval.UpdatedAt,
		})
	case http.MethodPost:
		payload := struct {
			Approved bool `json:"approved"`
		}{}
		if decodeError := json.NewDecoder(request.Body).Decode(&payload); decodeError != nil {
			writeJSON(writer, http.StatusBadRequest, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: fmt.Sprintf("invalid approval body: %v", decodeError)})
			return
		}

		if approval.Status != "pending" {
			writeJSON(writer, http.StatusConflict, runResponse{
				MustUseSmartsh: true,
				Status:         "blocked",
				Executed:       false,
				ExitCode:       1,
				ApprovalID:     approval.ID,
				Error:          fmt.Sprintf("approval is already %s", approval.Status),
			})
			return
		}

		if !payload.Approved {
			approval.Status = "rejected"
			approval.UpdatedAt = time.Now()
			_ = server.store.SaveApproval(*approval)
			if approval.JobID != "" {
				server.updateJobWithApprovalResult(approval.JobID, runResponse{
					MustUseSmartsh:  true,
					JobID:           approval.JobID,
					Status:          "blocked",
					Executed:        false,
					ResolvedCommand: approval.ResolvedCommand,
					ExitCode:        1,
					ErrorType:       "policy",
					BlockedReason:   "risky command rejected by user",
					ApprovalID:      approval.ID,
					Error:           "approval rejected",
				})
			}
			writeJSON(writer, http.StatusOK, runResponse{
				MustUseSmartsh:  true,
				JobID:           approval.JobID,
				Status:          "blocked",
				Executed:        false,
				ResolvedCommand: approval.ResolvedCommand,
				ExitCode:        1,
				ErrorType:       "policy",
				BlockedReason:   "risky command rejected by user",
				ApprovalID:      approval.ID,
				Error:           "approval rejected",
			})
			return
		}

		approval.Status = "approved"
		approval.UpdatedAt = time.Now()
		_ = server.store.SaveApproval(*approval)
		if approval.JobID != "" {
			running := runResponse{
				MustUseSmartsh:  true,
				JobID:           approval.JobID,
				Status:          "running",
				Executed:        false,
				ResolvedCommand: approval.ResolvedCommand,
				ExitCode:        0,
				Summary:         "approval accepted; executing command",
				ApprovalID:      approval.ID,
			}
			server.updateJobWithApprovalResult(approval.JobID, running)
			go server.executeApprovedJob(*approval)
			writeJSON(writer, http.StatusAccepted, running)
			return
		}

		result := server.executeApprovalNow(request.Context(), *approval)
		writeJSON(writer, http.StatusOK, result)
	default:
		writeJSON(writer, http.StatusMethodNotAllowed, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "method not allowed"})
	}
}

func (server *daemonServer) handleJobByID(writer http.ResponseWriter, request *http.Request, jobID string) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "method not allowed"})
		return
	}
	job, err := server.store.Get(jobID)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: err.Error()})
		return
	}
	if job == nil {
		writeJSON(writer, http.StatusNotFound, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "job not found"})
		return
	}
	writeJSON(writer, http.StatusOK, job.Result)
}

func (server *daemonServer) handleJobStream(writer http.ResponseWriter, request *http.Request, jobID string) {
	if request.Method != http.MethodGet {
		writeJSON(writer, http.StatusMethodNotAllowed, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "method not allowed"})
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSON(writer, http.StatusInternalServerError, runResponse{MustUseSmartsh: true, Executed: false, ExitCode: 1, Error: "streaming not supported"})
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	initial, err := server.store.Get(jobID)
	if err != nil || initial == nil {
		io.WriteString(writer, "event: error\ndata: {\"error\":\"job not found\"}\n\n")
		flusher.Flush()
		return
	}
	sendSSE(writer, "status", initial.Result)
	flusher.Flush()
	if isTerminalStatus(initial.Result.Status) {
		return
	}

	channel := make(chan runResponse, 8)
	server.subscribe(jobID, channel)
	defer server.unsubscribe(jobID, channel)

	heartbeat := time.NewTicker(12 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case response := <-channel:
			sendSSE(writer, "status", response)
			flusher.Flush()
			if isTerminalStatus(response.Status) {
				return
			}
		case <-heartbeat.C:
			latest, latestErr := server.store.Get(jobID)
			if latestErr == nil && latest != nil && isTerminalStatus(latest.Result.Status) {
				sendSSE(writer, "status", latest.Result)
				flusher.Flush()
				return
			}
			io.WriteString(writer, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (server *daemonServer) executeJob(jobID string) {
	job, err := server.store.Get(jobID)
	if err != nil || job == nil {
		return
	}
	job.Result.Status = "running"
	job.Result.Summary = "job running"
	job.UpdatedAt = time.Now()
	_ = server.store.Save(*job)
	server.publish(job.ID, job.Result)

	result := server.executeRequest(context.Background(), job.Request, job.ID)
	result.JobID = job.ID
	if result.Status == "" {
		if result.Error != "" && result.ExitCode != 0 {
			result.Status = "failed"
		} else {
			result.Status = "completed"
		}
	}
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = server.store.Save(*job)
	server.publish(job.ID, result)
	server.metrics.recordRun(result)
	server.metrics.recordJobStatus(result.Status)
}

func (server *daemonServer) executeRequest(ctx context.Context, runRequestPayload runRequest, jobID string) runResponse {
	startedAt := time.Now()
	cwd, cwdError := resolveWorkingDirectory(runRequestPayload.Cwd)
	if cwdError != nil {
		return runResponse{MustUseSmartsh: true, Status: "failed", Executed: false, ExitCode: 1, Error: cwdError.Error()}
	}

	allowlistMode := strings.TrimSpace(runRequestPayload.AllowlistMode)
	if allowlistMode == "" {
		allowlistMode = string(security.AllowlistModeOff)
	}
	parsedAllowlistMode, allowlistModeError := security.ParseAllowlistMode(allowlistMode)
	if allowlistModeError != nil {
		return runResponse{MustUseSmartsh: true, Status: "failed", Executed: false, ExitCode: 1, Error: allowlistModeError.Error()}
	}

	var commandAllowlist *security.Allowlist
	if parsedAllowlistMode != security.AllowlistModeOff {
		allowlistFile := strings.TrimSpace(runRequestPayload.AllowlistFile)
		if allowlistFile == "" {
			allowlistFile = ".smartsh-allowlist"
		}
		loadedAllowlist, loadAllowlistError := security.LoadAllowlist(filepath.Join(cwd, allowlistFile))
		if loadAllowlistError != nil {
			if errors.Is(loadAllowlistError, os.ErrNotExist) && parsedAllowlistMode == security.AllowlistModeWarn {
				commandAllowlist = &security.Allowlist{}
			} else {
				return runResponse{MustUseSmartsh: true, Status: "failed", Executed: false, ExitCode: 1, Error: fmt.Sprintf("allowlist load failed: %v", loadAllowlistError)}
			}
		} else {
			commandAllowlist = loadedAllowlist
		}
	}

	resolvedCommand := strings.TrimSpace(runRequestPayload.Command)
	resolvedRisk := "low"
	if resolvedCommand == "" {
		return runResponse{MustUseSmartsh: true, Status: "failed", Executed: false, ExitCode: 1, Error: "command is required"}
	}

	commandAssessment, assessmentError := security.AssessCommand(resolvedCommand, strings.ToLower(resolvedRisk), runRequestPayload.Unsafe)
	if assessmentError != nil {
		return runResponse{
			MustUseSmartsh:  true,
			Status:          "blocked",
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        2,
			ErrorType:       "policy",
			BlockedReason:   assessmentError.Error(),
			Error:           "command blocked by safety policy",
		}
	}
	resolvedRisk = strings.ToLower(strings.TrimSpace(commandAssessment.RiskLevel))
	if resolvedRisk == "" {
		resolvedRisk = "low"
	}
	if commandAssessment.RequiresRiskConfirmation && !runRequestPayload.Unsafe {
		if !runRequestPayload.RequireApproval {
			return runResponse{
				MustUseSmartsh:  true,
				Status:          "blocked",
				Executed:        false,
				ResolvedCommand: resolvedCommand,
				ExitCode:        2,
				ErrorType:       "policy",
				BlockedReason:   fmt.Sprintf("risky command requires explicit unsafe approval: %s", commandAssessment.RiskReason),
				Error:           "command requires unsafe approval",
			}
		}
		riskTargets := extractRiskTargets(resolvedCommand, cwd)
		approval := commandApproval{
			ID:              fmt.Sprintf("approval_%d", time.Now().UnixNano()),
			JobID:           jobID,
			Request:         runRequestPayload,
			ResolvedCommand: resolvedCommand,
			ResolvedRisk:    resolvedRisk,
			RiskReason:      commandAssessment.RiskReason,
			RiskTargets:     riskTargets,
			Status:          "pending",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if saveApprovalError := server.store.SaveApproval(approval); saveApprovalError != nil {
			return runResponse{
				MustUseSmartsh:  true,
				Status:          "failed",
				Executed:        false,
				ResolvedCommand: resolvedCommand,
				ExitCode:        1,
				Error:           fmt.Sprintf("failed to save approval request: %v", saveApprovalError),
			}
		}
		return runResponse{
			MustUseSmartsh:   true,
			JobID:            jobID,
			Status:           "needs_approval",
			Executed:         false,
			ResolvedCommand:  resolvedCommand,
			ExitCode:         0,
			ErrorType:        "policy",
			RequiresApproval: true,
			ApprovalID:       approval.ID,
			ApprovalMessage:  "risky command requires explicit approval before execution",
			ApprovalHowTo:    fmt.Sprintf(`call smartsh_approve with {"approval_id":"%s","decision":"yes"} or {"approval_id":"%s","decision":"no"}`, approval.ID, approval.ID),
			RiskReason:       commandAssessment.RiskReason,
			RiskTargets:      riskTargets,
			BlockedReason:    fmt.Sprintf("approval required: %s", commandAssessment.RiskReason),
		}
	}
	if _, allowlistValidationError := security.ValidateAllowlist(resolvedCommand, commandAllowlist, parsedAllowlistMode); allowlistValidationError != nil {
		return runResponse{
			MustUseSmartsh:  true,
			Status:          "blocked",
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        2,
			ErrorType:       "policy",
			BlockedReason:   allowlistValidationError.Error(),
			Error:           "command blocked by allowlist policy",
		}
	}

	policy, policyError := loadPolicy(cwd)
	if policyError != nil && (policy == nil || policy.Enforce) {
		return runResponse{
			MustUseSmartsh:  true,
			Status:          "blocked",
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        2,
			ErrorType:       "policy",
			BlockedReason:   policyError.Error(),
			Error:           "command blocked by policy parse failure",
		}
	}
	if applyError := applyPolicy(policy, cwd, resolvedCommand, resolvedRisk); applyError != nil {
		return runResponse{
			MustUseSmartsh:  true,
			Status:          "blocked",
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        2,
			ErrorType:       "policy",
			BlockedReason:   applyError.Error(),
			Error:           "command blocked by .smartsh-policy.yaml",
		}
	}

	if runRequestPayload.DryRun {
		return runResponse{
			MustUseSmartsh:  true,
			Status:          "completed",
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        0,
			ErrorType:       "none",
			Summary:         "dry run: command resolved and validated",
			SummarySource:   "deterministic",
			DurationMS:      time.Since(startedAt).Milliseconds(),
		}
	}

	executionContext := ctx
	cancel := func() {}
	timeoutSec := runRequestPayload.TimeoutSec
	if timeoutSec > 0 {
		executionContext, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	}
	defer cancel()

	isolation := isolationOptions{
		Isolated:      runRequestPayload.Isolated || !runRequestPayload.Unsafe,
		MaxOutputKB:   runRequestPayload.MaxOutputKB,
		MaxMemoryMB:   runRequestPayload.MaxMemoryMB,
		MaxCPUSeconds: runRequestPayload.MaxCPUSeconds,
		AllowedEnv:    runRequestPayload.AllowedEnv,
		Env:           runRequestPayload.Env,
	}
	if isolation.MaxOutputKB <= 0 {
		isolation.MaxOutputKB = defaultRunMaxOutputKB
	}

	env := buildEnvWithPolicy(policy, runRequestPayload)
	exitCode := 0
	combinedOutput := ""
	var executionError error
	if runRequestPayload.OpenExternalTerminal || parseBooleanEnv("SMARTSH_OPEN_EXTERNAL_TERMINAL") {
		exitCode, combinedOutput, executionError = runCommandViaExternalTerminal(
			executionContext,
			resolvedCommand,
			cwd,
			isolation,
			env,
			runRequestPayload.TerminalApp,
			runRequestPayload.TerminalSessionKey,
		)
	} else {
		exitCode, combinedOutput, executionError = runCommandWithCapture(executionContext, resolvedCommand, cwd, isolation, env)
	}
	summaryResult := resolveSummary(resolvedCommand, exitCode, combinedOutput, executionError, server.httpClient)
	resolvedSummary := summaryResult.Summary

	response := runResponse{
		MustUseSmartsh:  true,
		Status:          "completed",
		Executed:        true,
		ResolvedCommand: resolvedCommand,
		ExitCode:        exitCode,
		Summary:         resolvedSummary.Summary,
		SummarySource:   summaryResult.Source,
		ErrorType:       resolvedSummary.ErrorType,
		PrimaryError:    resolvedSummary.PrimaryError,
		NextAction:      resolvedSummary.NextAction,
		FailingTests:    resolvedSummary.FailingTests,
		FailedFiles:     resolvedSummary.FailedFiles,
		TopIssues:       resolvedSummary.TopIssues,
		DurationMS:      time.Since(startedAt).Milliseconds(),
	}
	if executionError != nil {
		response.Error = executionError.Error()
		if response.ErrorType == "" {
			response.ErrorType = "runtime"
		}
		response.Status = "failed"
	}
	if response.Status == "failed" {
		response.OutputTail = tailString(combinedOutput, failedRunOutputTailMaxSize)
	}
	return response
}

func (server *daemonServer) executeApprovalNow(ctx context.Context, approval commandApproval) runResponse {
	approvedRequest := approval.Request
	approvedRequest.Command = approval.ResolvedCommand
	approvedRequest.RequireApproval = false
	approvedRequest.Unsafe = true
	response := server.executeRequest(ctx, approvedRequest, approval.JobID)
	response.ApprovalID = approval.ID
	response.RequiresApproval = false

	latestApproval, approvalError := server.store.GetApproval(approval.ID)
	if approvalError == nil && latestApproval != nil {
		if response.ExitCode == 0 && response.Error == "" {
			latestApproval.Status = "executed"
		} else {
			latestApproval.Status = "approved_failed"
		}
		latestApproval.UpdatedAt = time.Now()
		_ = server.store.SaveApproval(*latestApproval)
	}
	return response
}

func (server *daemonServer) executeApprovedJob(approval commandApproval) {
	result := server.executeApprovalNow(context.Background(), approval)
	if result.JobID == "" {
		result.JobID = approval.JobID
	}
	if result.Status == "" {
		if result.Error != "" && result.ExitCode != 0 {
			result.Status = "failed"
		} else {
			result.Status = "completed"
		}
	}
	server.updateJobWithApprovalResult(approval.JobID, result)
	server.metrics.recordRun(result)
	server.metrics.recordJobStatus(result.Status)
}

func (server *daemonServer) updateJobWithApprovalResult(jobID string, result runResponse) {
	if strings.TrimSpace(jobID) == "" {
		return
	}
	job, jobError := server.store.Get(jobID)
	if jobError != nil || job == nil {
		return
	}
	job.Result = result
	job.UpdatedAt = time.Now()
	_ = server.store.Save(*job)
	server.publish(job.ID, result)
}

func (server *daemonServer) handleSessions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"must_use_smartsh": true, "error": "method not allowed"})
		return
	}
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"must_use_smartsh": true, "error": "unauthorized"})
		return
	}
	payload := ptyCreateRequest{}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"must_use_smartsh": true, "error": err.Error()})
		return
	}
	response, statusCode, err := server.createPTYSession(request.Context(), payload)
	if err != nil {
		writeJSON(writer, statusCode, map[string]any{"must_use_smartsh": true, "error": err.Error()})
		return
	}
	writeJSON(writer, statusCode, response)
}

func (server *daemonServer) handleSessionRoutes(writer http.ResponseWriter, request *http.Request) {
	if !server.authorize(request) {
		writeJSON(writer, http.StatusUnauthorized, map[string]any{"must_use_smartsh": true, "error": "unauthorized"})
		return
	}
	path := strings.TrimPrefix(request.URL.Path, "/sessions/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"must_use_smartsh": true, "error": "session id is required"})
		return
	}
	sessionID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	server.ptySessionsMutex.Lock()
	session := server.ptySessions[sessionID]
	server.ptySessionsMutex.Unlock()
	if session == nil {
		writeJSON(writer, http.StatusNotFound, map[string]any{"must_use_smartsh": true, "error": "session not found"})
		return
	}

	switch action {
	case "input":
		if request.Method != http.MethodPost {
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"must_use_smartsh": true, "error": "method not allowed"})
			return
		}
		payload := struct {
			Data string `json:"data"`
		}{}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]any{"must_use_smartsh": true, "error": err.Error()})
			return
		}
		session.mu.Lock()
		_, err := session.file.WriteString(payload.Data)
		session.mu.Unlock()
		if err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]any{"must_use_smartsh": true, "error": err.Error()})
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"must_use_smartsh": true, "ok": true})
	case "stream":
		if request.Method != http.MethodGet {
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"must_use_smartsh": true, "error": "method not allowed"})
			return
		}
		flusher, ok := writer.(http.Flusher)
		if !ok {
			writeJSON(writer, http.StatusInternalServerError, map[string]any{"must_use_smartsh": true, "error": "streaming unsupported"})
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.Header().Set("Cache-Control", "no-cache")
		writer.Header().Set("Connection", "keep-alive")
		session.mu.Lock()
		io.WriteString(writer, "event: output\ndata: "+jsonEscape(session.OutputTail)+"\n\n")
		channel := make(chan string, 32)
		session.subscribers[channel] = struct{}{}
		currentStatus := session.Status
		session.mu.Unlock()
		flusher.Flush()
		if currentStatus != "running" {
			return
		}
		defer func() {
			session.mu.Lock()
			delete(session.subscribers, channel)
			session.mu.Unlock()
		}()
		heartbeat := time.NewTicker(10 * time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case <-request.Context().Done():
				return
			case chunk, open := <-channel:
				if !open {
					return
				}
				io.WriteString(writer, "event: output\ndata: "+jsonEscape(chunk)+"\n\n")
				flusher.Flush()
			case <-heartbeat.C:
				io.WriteString(writer, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	case "close":
		if request.Method != http.MethodPost {
			writeJSON(writer, http.StatusMethodNotAllowed, map[string]any{"must_use_smartsh": true, "error": "method not allowed"})
			return
		}
		session.cancel()
		writeJSON(writer, http.StatusOK, map[string]any{"must_use_smartsh": true, "ok": true})
	default:
		writeJSON(writer, http.StatusOK, map[string]any{
			"must_use_smartsh": true,
			"session_id":       sessionID,
			"status":           session.Status,
			"exit_code":        session.ExitCode,
			"output_tail":      tailString(session.OutputTail, 2000),
			"summary":          session.ResolvedSummary,
		})
	}
}

func (server *daemonServer) handleMetrics(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !server.authorize(request) {
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = writer.Write([]byte(server.metrics.renderPrometheus()))
}

func runCommandWithCapture(ctx context.Context, command string, cwd string, isolation isolationOptions, env []string) (int, string, error) {
	var execCommand *exec.Cmd
	finalCommand := command
	if runtime.GOOS != "windows" && isolation.Isolated {
		finalCommand = wrapWithULimits(command, isolation)
	}
	if runtime.GOOS == "windows" {
		execCommand = exec.CommandContext(ctx, "cmd", "/C", finalCommand)
	} else {
		execCommand = exec.CommandContext(ctx, "sh", "-c", finalCommand)
	}
	execCommand.Dir = cwd
	execCommand.Env = env

	var outputBuffer bytes.Buffer
	limitWriter := &limitedBufferWriter{
		maxBytes: int64(max(1, isolation.MaxOutputKB) * 1024),
		buffer:   &outputBuffer,
	}
	execCommand.Stdout = limitWriter
	execCommand.Stderr = limitWriter
	outputError := execCommand.Run()

	exitCode := 0
	if outputError != nil {
		var exitError *exec.ExitError
		if strings.Contains(outputError.Error(), "exit status ") && errors.As(outputError, &exitError) {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = 1
		}
	}
	return exitCode, outputBuffer.String(), outputError
}

func wrapWithULimits(command string, isolation isolationOptions) string {
	limits := make([]string, 0, 2)
	if isolation.MaxCPUSeconds > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -t %d", isolation.MaxCPUSeconds))
	}
	if isolation.MaxMemoryMB > 0 {
		limits = append(limits, fmt.Sprintf("ulimit -v %d", isolation.MaxMemoryMB*1024))
	}
	if len(limits) == 0 {
		return command
	}
	return strings.Join(limits, "; ") + "; " + command
}

func classifyErrorType(command string, output string, runError error, exitCode int) string {
	if exitCode == 0 && runError == nil {
		return "none"
	}
	combined := strings.ToLower(command + "\n" + output)
	compileTokens := []string{"failed to compile", "compilation failed", "syntax error", "error ts", "javac", "cannot find symbol", "build failed", "compile"}
	testTokens := []string{"test failed", "failing", "assert", "expected", "jest", "vitest", "pytest", "go test", "dotnet test", "--- fail"}
	dependencyTokens := []string{"npm err", "eresolve", "cannot resolve dependency", "module not found", "no matching distribution found", "dotnet restore", "mvn dependency", "could not resolve dependencies"}
	runtimeTokens := []string{"panic", "exception", "segmentation fault", "connection refused", "timeout", "traceback"}
	for _, token := range compileTokens {
		if strings.Contains(combined, token) {
			return "compile"
		}
	}
	for _, token := range testTokens {
		if strings.Contains(combined, token) {
			return "test"
		}
	}
	for _, token := range dependencyTokens {
		if strings.Contains(combined, token) {
			return "dependency"
		}
	}
	for _, token := range runtimeTokens {
		if strings.Contains(combined, token) {
			return "runtime"
		}
	}
	return "runtime"
}

func resolveWorkingDirectory(cwd string) (string, error) {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		current, getwdError := os.Getwd()
		if getwdError != nil {
			return "", fmt.Errorf("getwd failed: %w", getwdError)
		}
		return current, nil
	}
	absolutePath, absoluteError := filepath.Abs(trimmed)
	if absoluteError != nil {
		return "", fmt.Errorf("invalid cwd: %w", absoluteError)
	}
	stat, statError := os.Stat(absolutePath)
	if statError != nil {
		return "", fmt.Errorf("cwd not found: %w", statError)
	}
	if !stat.IsDir() {
		return "", fmt.Errorf("cwd is not a directory: %s", absolutePath)
	}
	return absolutePath, nil
}

func splitNonEmptyLines(text string) []string {
	rawLines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func tailString(text string, maxLength int) string {
	if maxLength <= 0 {
		return ""
	}
	if len(text) <= maxLength {
		return text
	}
	return text[len(text)-maxLength:]
}

type limitedBufferWriter struct {
	maxBytes  int64
	buffer    *bytes.Buffer
	written   int64
	truncated bool
}

func (writer *limitedBufferWriter) Write(data []byte) (int, error) {
	totalLen := len(data)
	if writer.maxBytes <= 0 {
		return totalLen, nil
	}
	remaining := writer.maxBytes - writer.written
	if remaining <= 0 {
		if !writer.truncated {
			writer.buffer.WriteString("\n[smartshd output truncated]\n")
			writer.truncated = true
		}
		return totalLen, nil
	}
	writeLen := len(data)
	if int64(writeLen) > remaining {
		writeLen = int(remaining)
	}
	if writeLen > 0 {
		_, _ = writer.buffer.Write(data[:writeLen])
		writer.written += int64(writeLen)
	}
	if writeLen < totalLen && !writer.truncated {
		writer.buffer.WriteString("\n[smartshd output truncated]\n")
		writer.truncated = true
	}
	return totalLen, nil
}

func (server *daemonServer) authorize(request *http.Request) bool {
	if server.authDisabled {
		return true
	}
	token := strings.TrimSpace(server.daemonToken)
	if token == "" {
		return false
	}
	headerToken := strings.TrimSpace(request.Header.Get("X-Smartsh-Token"))
	if headerToken != "" && headerToken == token {
		return true
	}
	authHeader := strings.TrimSpace(request.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		if strings.TrimSpace(authHeader[len("Bearer "):]) == token {
			return true
		}
	}
	return false
}

func resolveDaemonAuthConfig() (bool, string) {
	configValues := map[string]string{}
	config, configErr := runtimeconfig.Load("")
	if configErr == nil {
		configValues = config.Values
	}
	authDisabled := runtimeconfig.ResolveBool("SMARTSH_DAEMON_DISABLE_AUTH", configValues)
	daemonToken := runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues)
	return authDisabled, daemonToken
}

func (server *daemonServer) subscribe(jobID string, channel chan runResponse) {
	server.subscribersMutex.Lock()
	defer server.subscribersMutex.Unlock()
	if _, exists := server.subscribers[jobID]; !exists {
		server.subscribers[jobID] = map[chan runResponse]struct{}{}
	}
	server.subscribers[jobID][channel] = struct{}{}
}

func (server *daemonServer) unsubscribe(jobID string, channel chan runResponse) {
	server.subscribersMutex.Lock()
	defer server.subscribersMutex.Unlock()
	if existing, exists := server.subscribers[jobID]; exists {
		delete(existing, channel)
		close(channel)
		if len(existing) == 0 {
			delete(server.subscribers, jobID)
		}
	}
}

func (server *daemonServer) publish(jobID string, response runResponse) {
	server.subscribersMutex.Lock()
	defer server.subscribersMutex.Unlock()
	for channel := range server.subscribers[jobID] {
		select {
		case channel <- response:
		default:
		}
	}
}

func isTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "blocked", "needs_approval":
		return true
	default:
		return false
	}
}

func sendSSE(writer io.Writer, event string, payload any) {
	payloadBytes, _ := json.Marshal(payload)
	_, _ = io.WriteString(writer, "event: "+event+"\n")
	_, _ = io.WriteString(writer, "data: "+string(payloadBytes)+"\n\n")
}

func jsonEscape(value string) string {
	raw, _ := json.Marshal(value)
	if len(raw) >= 2 {
		return string(raw[1 : len(raw)-1])
	}
	return ""
}

func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(statusCode)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func extractRiskTargets(command string, cwd string) []string {
	targets := make([]string, 0, 3)
	trimmedCommand := strings.TrimSpace(command)
	if trimmedCommand == "" {
		return []string{cwd}
	}

	for _, token := range strings.Fields(trimmedCommand) {
		candidate := strings.TrimSpace(token)
		if candidate == "" || strings.HasPrefix(candidate, "-") {
			continue
		}
		if strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "./") || strings.HasPrefix(candidate, "../") {
			targets = append(targets, candidate)
		}
	}

	if len(targets) == 0 {
		targets = append(targets, cwd)
	}
	return targets
}
