package security

import (
	"regexp"
)

var blockedPatterns = []struct {
	reason string
	regex  *regexp.Regexp
}{
	{reason: "system wipe command", regex: regexp.MustCompile(`(?i)\brm\s+-rf\s+/(\s|$)`)},
	{reason: "system wipe command", regex: regexp.MustCompile(`(?i)\bmkfs(\.[a-z0-9]+)?\b`)},
	{reason: "destructive raw disk write", regex: regexp.MustCompile(`(?i)\bdd\s+if=`)},
	{reason: "privilege escalation", regex: regexp.MustCompile(`(?i)\bsudo\b`)},
	{reason: "privilege escalation", regex: regexp.MustCompile(`(?i)\bsu\b`)},
	{reason: "shutdown or reboot command", regex: regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`)},
	{reason: "pipe-to-shell pattern", regex: regexp.MustCompile(`(?i)\|\s*(sh|bash|zsh|powershell|pwsh|cmd)(\s|$)`)},
	{reason: "dangerous download and execute", regex: regexp.MustCompile(`(?i)\b(curl|wget).*\|\s*(sh|bash|zsh|powershell|pwsh|cmd)\b`)},
}

var suspiciousPatterns = []struct {
	reason string
	regex  *regexp.Regexp
}{
	{reason: "recursive delete", regex: regexp.MustCompile(`(?i)\brm\s+-rf\b`)},
	{reason: "force delete", regex: regexp.MustCompile(`(?i)\b(del|erase)\s+/f\b`)},
	{reason: "git hard reset", regex: regexp.MustCompile(`(?i)\bgit\s+reset\s+--hard\b`)},
	{reason: "dangerous chmod", regex: regexp.MustCompile(`(?i)\bchmod\s+777\b`)},
}

func ValidateCommand(command string, risk string, allowUnsafe bool) error {
	_, assessmentError := AssessCommand(command, risk, allowUnsafe)
	return assessmentError
}
