package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateCommand_BlockedAndUnsafe(t *testing.T) {
	t.Parallel()

	blockedError := ValidateCommand("rm -rf /", "low", false)
	if blockedError == nil || !strings.Contains(blockedError.Error(), "blocked:") {
		t.Fatalf("expected blocked command error")
	}

	unsafeAllowedError := ValidateCommand("rm -rf /", "low", true)
	if unsafeAllowedError != nil {
		t.Fatalf("expected unsafe command to pass with --unsafe, got %v", unsafeAllowedError)
	}
}

func TestValidateCommand_HighRiskBlockedWithoutUnsafe(t *testing.T) {
	t.Parallel()

	assessment, assessmentError := AssessCommand("echo hello", "high", false)
	if assessmentError != nil {
		t.Fatalf("did not expect hard block for high risk alone, got %v", assessmentError)
	}
	if !assessment.RequiresRiskConfirmation {
		t.Fatalf("expected high-risk command to require confirmation")
	}
	if assessment.RiskLevel != "high" {
		t.Fatalf("expected risk level high, got %q", assessment.RiskLevel)
	}
}

func TestAllowlistModes(t *testing.T) {
	t.Parallel()

	allowlist := &Allowlist{
		entries: []allowlistEntry{
			{kind: "exact", value: "go test ./..."},
			{kind: "prefix", value: "npm run "},
		},
	}

	if warning, validationError := ValidateAllowlist("go test ./...", allowlist, AllowlistModeEnforce); validationError != nil || warning != "" {
		t.Fatalf("expected exact allowlist match to pass, warning=%q error=%v", warning, validationError)
	}

	warning, validationError := ValidateAllowlist("go build ./...", allowlist, AllowlistModeWarn)
	if validationError != nil {
		t.Fatalf("expected warn mode to avoid blocking, got %v", validationError)
	}
	if warning == "" {
		t.Fatalf("expected warning in warn mode")
	}

	_, enforceError := ValidateAllowlist("go build ./...", allowlist, AllowlistModeEnforce)
	if enforceError == nil {
		t.Fatalf("expected enforce mode to block unlisted command")
	}
}

func TestLoadAllowlistAndMatchRegex(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	allowlistPath := filepath.Join(tempDir, ".smartsh-allowlist")
	fileContents := strings.Join([]string{
		"# smartsh allowlist",
		"exact:go test ./...",
		"prefix:npm run ",
		"re:^docker compose (up|build)$",
		"",
	}, "\n")
	if writeError := os.WriteFile(allowlistPath, []byte(fileContents), 0o600); writeError != nil {
		t.Fatalf("write allowlist: %v", writeError)
	}

	allowlist, loadError := LoadAllowlist(allowlistPath)
	if loadError != nil {
		t.Fatalf("load allowlist: %v", loadError)
	}

	if !allowlist.Matches("go test ./...") {
		t.Fatalf("expected exact command to match")
	}
	if !allowlist.Matches("npm run dev") {
		t.Fatalf("expected prefix command to match")
	}
	if !allowlist.Matches("docker compose build") {
		t.Fatalf("expected regex command to match")
	}
	if allowlist.Matches("python3 -m pytest") {
		t.Fatalf("did not expect unrelated command to match")
	}
}

func TestParseAllowlistMode(t *testing.T) {
	t.Parallel()

	modes := []AllowlistMode{AllowlistModeOff, AllowlistModeWarn, AllowlistModeEnforce}
	for _, expectedMode := range modes {
		mode, parseError := ParseAllowlistMode(string(expectedMode))
		if parseError != nil {
			t.Fatalf("unexpected parse error for %s: %v", expectedMode, parseError)
		}
		if mode != expectedMode {
			t.Fatalf("expected mode %s, got %s", expectedMode, mode)
		}
	}

	if _, parseError := ParseAllowlistMode("invalid"); parseError == nil {
		t.Fatalf("expected invalid mode parse error")
	}
}
