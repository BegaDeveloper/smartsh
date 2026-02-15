package main

import "time"

type runRequest struct {
	Command              string            `json:"command,omitempty"`
	Cwd                  string            `json:"cwd,omitempty"`
	OpenExternalTerminal bool              `json:"open_external_terminal,omitempty"`
	TerminalApp          string            `json:"terminal_app,omitempty"`
	TerminalSessionKey   string            `json:"terminal_session_key,omitempty"`
	Unsafe               bool              `json:"unsafe,omitempty"`
	RequireApproval      bool              `json:"require_approval,omitempty"`
	DryRun               bool              `json:"dry_run,omitempty"`
	Async                bool              `json:"async,omitempty"`
	TimeoutSec           int               `json:"timeout_sec,omitempty"`
	AllowlistMode        string            `json:"allowlist_mode,omitempty"`
	AllowlistFile        string            `json:"allowlist_file,omitempty"`
	Isolated             bool              `json:"isolated,omitempty"`
	MaxOutputKB          int               `json:"max_output_kb,omitempty"`
	MaxMemoryMB          int               `json:"max_memory_mb,omitempty"`
	MaxCPUSeconds        int               `json:"max_cpu_seconds,omitempty"`
	AllowedEnv           []string          `json:"allowed_env,omitempty"`
	Env                  map[string]string `json:"env,omitempty"`
}

type runResponse struct {
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

type daemonJob struct {
	ID        string      `json:"id"`
	Request   runRequest  `json:"request"`
	Result    runResponse `json:"result"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

type commandApproval struct {
	ID              string     `json:"id"`
	JobID           string     `json:"job_id,omitempty"`
	Request         runRequest `json:"request"`
	ResolvedCommand string     `json:"resolved_command"`
	ResolvedRisk    string     `json:"resolved_risk"`
	RiskReason      string     `json:"risk_reason"`
	RiskTargets     []string   `json:"risk_targets,omitempty"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type isolationOptions struct {
	Isolated      bool
	MaxOutputKB   int
	MaxMemoryMB   int
	MaxCPUSeconds int
	AllowedEnv    []string
	Env           map[string]string
}
