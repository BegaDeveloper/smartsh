package executor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func ConfirmExecution(command string, autoConfirm bool) (bool, error) {
	if autoConfirm {
		return true, nil
	}

	if !isInteractiveTerminal() {
		return false, fmt.Errorf("non-interactive terminal; pass --yes to continue")
	}

	fmt.Printf("Execute command? [y/N] %s\n> ", command)
	reader := bufio.NewReader(os.Stdin)
	input, readError := reader.ReadString('\n')
	if readError != nil && !errors.Is(readError, os.ErrClosed) {
		return false, readError
	}

	normalized := strings.ToLower(strings.TrimSpace(input))
	return normalized == "y" || normalized == "yes", nil
}

func ConfirmRiskyExecution(command string, reason string, forcePrompt bool) (bool, error) {
	if !forcePrompt {
		return true, nil
	}
	if !isInteractiveTerminal() {
		return false, fmt.Errorf("risky command requires interactive confirmation")
	}

	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = "risky operation detected"
	}
	fmt.Printf("WARNING: %s\n", trimmedReason)
	fmt.Printf("You may be deleting/changing critical resources.\nProceed? [y/N] %s\n> ", command)

	reader := bufio.NewReader(os.Stdin)
	input, readError := reader.ReadString('\n')
	if readError != nil && !errors.Is(readError, os.ErrClosed) {
		return false, readError
	}

	normalized := strings.ToLower(strings.TrimSpace(input))
	return normalized == "y" || normalized == "yes", nil
}

func RunStreaming(ctx context.Context, command string) (int, error) {
	execCommand := buildShellCommand(ctx, command)
	execCommand.Stdout = os.Stdout
	execCommand.Stderr = os.Stderr
	execCommand.Stdin = os.Stdin

	runError := execCommand.Run()
	if runError == nil {
		return 0, nil
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return 130, context.Canceled
	}

	var exitError *exec.ExitError
	if errors.As(runError, &exitError) {
		if statusCode := extractExitCode(exitError); statusCode >= 0 {
			return statusCode, runError
		}
		return 1, runError
	}

	return 1, runError
}

func buildShellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func extractExitCode(exitError *exec.ExitError) int {
	if exitError == nil {
		return -1
	}

	if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
		return status.ExitStatus()
	}

	message := exitError.Error()
	segments := strings.Split(message, "exit status ")
	if len(segments) > 1 {
		if parsed, parseError := strconv.Atoi(strings.TrimSpace(segments[len(segments)-1])); parseError == nil {
			return parsed
		}
	}
	return -1
}

func isInteractiveTerminal() bool {
	stdinInfo, stdinError := os.Stdin.Stat()
	if stdinError != nil {
		return false
	}
	return (stdinInfo.Mode() & os.ModeCharDevice) != 0
}
