package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var externalTerminalRunMutex sync.Mutex

func runCommandViaExternalTerminal(
	ctx context.Context,
	command string,
	cwd string,
	isolation isolationOptions,
	env []string,
	terminalApp string,
	terminalSessionKey string,
) (int, string, error) {
	// Reusing one external terminal session requires sequential execution.
	externalTerminalRunMutex.Lock()
	defer externalTerminalRunMutex.Unlock()

	effectiveCommand := command
	if isolation.Isolated {
		effectiveCommand = wrapWithULimits(command, isolation)
	}

	tempDir, err := os.MkdirTemp("", "smartsh-ext-terminal-*")
	if err != nil {
		return 1, "", err
	}
	defer os.RemoveAll(tempDir)

	outputPath := filepath.Join(tempDir, "output.log")
	exitCodePath := filepath.Join(tempDir, "exit.code")
	pidPath := filepath.Join(tempDir, "shell.pid")

	launchErr := error(nil)
	switch runtime.GOOS {
	case "darwin":
		scriptPath := filepath.Join(tempDir, "run.sh")
		scriptContent := buildExternalTerminalScript(
			cwd,
			effectiveCommand,
			env,
			outputPath,
			exitCodePath,
			pidPath,
		)
		if writeErr := os.WriteFile(scriptPath, []byte(scriptContent), 0o700); writeErr != nil {
			return 1, "", writeErr
		}
		launchErr = launchExternalTerminal(scriptPath, terminalApp, terminalSessionKey)
	case "windows":
		scriptPath := filepath.Join(tempDir, "run.ps1")
		scriptContent := buildExternalTerminalPowerShellScript(
			cwd,
			effectiveCommand,
			env,
			outputPath,
			exitCodePath,
			pidPath,
		)
		if writeErr := os.WriteFile(scriptPath, []byte(scriptContent), 0o644); writeErr != nil {
			return 1, "", writeErr
		}
		launchErr = launchExternalTerminalWindows(scriptPath)
	default:
		return 1, "", fmt.Errorf("open_external_terminal is supported on macOS and Windows")
	}
	if launchErr != nil {
		return 1, "", launchErr
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			terminateExternalShell(pidPath)
			output, _ := readOutputWithLimit(outputPath, isolation.MaxOutputKB)
			return 1, output, fmt.Errorf("external terminal command timed out")
		case <-ticker.C:
			if _, statErr := os.Stat(exitCodePath); statErr == nil {
				output, _ := readOutputWithLimit(outputPath, isolation.MaxOutputKB)
				exitCode, readErr := readExitCode(exitCodePath)
				if readErr != nil {
					return 1, output, readErr
				}
				if exitCode == 0 {
					return 0, output, nil
				}
				return exitCode, output, fmt.Errorf("exit status %d", exitCode)
			}
		}
	}
}

func buildExternalTerminalScript(
	cwd string,
	command string,
	env []string,
	outputPath string,
	exitCodePath string,
	pidPath string,
) string {
	lines := []string{
		"#!/bin/sh",
		"set +e",
		"echo $$ > " + shellQuote(pidPath),
		"cd " + shellQuote(cwd) + " || exit 1",
	}
	for _, envEntry := range env {
		parts := strings.SplitN(envEntry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		lines = append(lines, "export "+parts[0]+"="+shellQuote(parts[1]))
	}
	lines = append(lines,
		"sh -lc "+shellQuote(command)+" > "+shellQuote(outputPath)+" 2>&1",
		"status=$?",
		"echo \"$status\" > "+shellQuote(exitCodePath),
		"exit \"$status\"",
	)
	return strings.Join(lines, "\n") + "\n"
}

func buildExternalTerminalPowerShellScript(
	cwd string,
	command string,
	env []string,
	outputPath string,
	exitCodePath string,
	pidPath string,
) string {
	lines := []string{
		"$ErrorActionPreference = 'Continue'",
		"Set-Content -Path '" + powerShellEscape(pidPath) + "' -Value $PID",
		"Set-Location -Path '" + powerShellEscape(cwd) + "'",
	}
	for _, envEntry := range env {
		parts := strings.SplitN(envEntry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		lines = append(lines, "$env:"+parts[0]+" = '"+powerShellEscape(parts[1])+"'")
	}
	lines = append(lines,
		"$output = & cmd /c '"+powerShellEscape(command)+"' 2>&1",
		"$output | Out-File -FilePath '"+powerShellEscape(outputPath)+"' -Encoding utf8",
		"$status = $LASTEXITCODE",
		"Set-Content -Path '"+powerShellEscape(exitCodePath)+"' -Value $status",
		"exit $status",
	)
	return strings.Join(lines, "\r\n") + "\r\n"
}

func launchExternalTerminal(scriptPath string, terminalApp string, terminalSessionKey string) error {
	commandToRun := "sh " + shellQuote(scriptPath)
	app := strings.ToLower(strings.TrimSpace(terminalApp))
	if app == "" {
		app = strings.ToLower(strings.TrimSpace(os.Getenv("SMARTSH_TERMINAL_APP")))
	}
	if app == "" || app == "terminal" || app == "terminal.app" {
		return launchTerminalAppWithReuse(commandToRun, terminalSessionKey)
	}
	if app == "iterm" || app == "iterm2" {
		return exec.Command(
			"osascript",
			"-e", `tell application "iTerm"`,
			"-e", "activate",
			"-e", `if (count of windows) = 0 then create window with default profile`,
			"-e", `tell current session of current window to write text "`+appleScriptEscape(commandToRun)+`"`,
			"-e", "end tell",
		).Run()
	}
	return fmt.Errorf("unsupported terminal_app %q, use terminal or iterm", terminalApp)
}

func launchTerminalAppWithReuse(commandToRun string, terminalSessionKey string) error {
	sessionKey := strings.TrimSpace(terminalSessionKey)
	if sessionKey == "" {
		sessionKey = "default"
	}
	stateFile := filepath.Join(os.TempDir(), "smartsh-terminal-session-"+sanitizeFileToken(sessionKey)+".window_id")
	existingWindowID := readTerminalWindowID(stateFile)
	script := buildTerminalReuseAppleScript(commandToRun, existingWindowID)
	output, runError := exec.Command("osascript", "-e", script).CombinedOutput()
	if runError != nil {
		return fmt.Errorf("terminal launch failed: %v (%s)", runError, strings.TrimSpace(string(output)))
	}
	updatedWindowID := strings.TrimSpace(string(output))
	if updatedWindowID != "" {
		_ = os.WriteFile(stateFile, []byte(updatedWindowID), 0o600)
	}
	return nil
}

func buildTerminalReuseAppleScript(commandToRun string, existingWindowID string) string {
	commandLiteral := appleScriptEscape(commandToRun)
	idLiteral := strings.TrimSpace(existingWindowID)
	if idLiteral == "" {
		return `tell application "Terminal"
activate
if not (exists window 1) then
	do script ""
end if
set targetWindow to front window
do script "` + commandLiteral + `" in selected tab of targetWindow
return (id of targetWindow) as string
end tell`
	}
	return `tell application "Terminal"
activate
if exists window id ` + idLiteral + ` then
	set targetWindow to window id ` + idLiteral + `
else
	if not (exists window 1) then
		do script ""
	end if
	set targetWindow to front window
end if
do script "` + commandLiteral + `" in selected tab of targetWindow
return (id of targetWindow) as string
end tell`
}

func readTerminalWindowID(path string) string {
	raw, readError := os.ReadFile(path)
	if readError != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return ""
	}
	if _, parseError := strconv.Atoi(trimmed); parseError != nil {
		return ""
	}
	return trimmed
}

func sanitizeFileToken(value string) string {
	builder := strings.Builder{}
	for _, character := range value {
		switch {
		case character >= 'a' && character <= 'z':
			builder.WriteRune(character)
		case character >= 'A' && character <= 'Z':
			builder.WriteRune(character + ('a' - 'A'))
		case character >= '0' && character <= '9':
			builder.WriteRune(character)
		case character == '-', character == '_':
			builder.WriteRune(character)
		default:
			builder.WriteRune('-')
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if normalized == "" {
		return "default"
	}
	return normalized
}

func launchExternalTerminalWindows(scriptPath string) error {
	return exec.Command(
		"cmd",
		"/C",
		"start",
		"",
		"powershell",
		"-NoExit",
		"-ExecutionPolicy",
		"Bypass",
		"-File",
		scriptPath,
	).Run()
}

func readOutputWithLimit(outputPath string, maxOutputKB int) (string, error) {
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return "", err
	}
	if maxOutputKB <= 0 {
		maxOutputKB = defaultRunMaxOutputKB
	}
	maxBytes := maxOutputKB * 1024
	if len(raw) <= maxBytes {
		return string(raw), nil
	}
	return string(raw[len(raw)-maxBytes:]) + "\n[smartshd output truncated]\n", nil
}

func readExitCode(exitCodePath string) (int, error) {
	raw, err := os.ReadFile(exitCodePath)
	if err != nil {
		return 1, err
	}
	parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil {
		return 1, parseErr
	}
	return parsed, nil
}

func terminateExternalShell(pidPath string) {
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	pid := strings.TrimSpace(string(raw))
	if pid == "" {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", pid, "/T", "/F").Run()
		return
	}
	_ = exec.Command("kill", "-TERM", pid).Run()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func appleScriptEscape(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return escaped
}

func powerShellEscape(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func parseBooleanEnv(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
