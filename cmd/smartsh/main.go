package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"smartsh/internal/ai"
	"smartsh/internal/detector"
	"smartsh/internal/executor"
	"smartsh/internal/resolver"
	"smartsh/internal/security"
)

const (
	exitSuccess     = 0
	exitFailure     = 1
	exitBlocked     = 2
	exitInterrupted = 130
)

type runResult struct {
	Executed        bool   `json:"executed"`
	ResolvedCommand string `json:"resolved_command"`
	ExitCode        int    `json:"exit_code"`
	Intent          string `json:"intent,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
	Risk            string `json:"risk,omitempty"`
	BlockedReason   string `json:"blocked_reason,omitempty"`
	AllowlistNotice string `json:"allowlist_notice,omitempty"`
	Error           string `json:"error,omitempty"`
}

type agentRequest struct {
	Instruction   string `json:"instruction"`
	Cwd           string `json:"cwd,omitempty"`
	Unsafe        *bool  `json:"unsafe,omitempty"`
	Yes           *bool  `json:"yes,omitempty"`
	DryRun        *bool  `json:"dry_run,omitempty"`
	AllowlistMode string `json:"allowlist_mode,omitempty"`
	AllowlistFile string `json:"allowlist_file,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	unsafeExecution := flag.Bool("unsafe", false, "allow risky commands")
	autoConfirm := flag.Bool("yes", false, "skip confirmation prompt")
	jsonMode := flag.Bool("json", false, "output machine-readable JSON")
	dryRun := flag.Bool("dry-run", false, "resolve and validate command without executing")
	agentMode := flag.Bool("agent", false, "agent mode: read instruction from args or stdin, force JSON output")
	explicitWorkingDirectory := flag.String("cwd", "", "working directory to execute in")
	debugAI := flag.Bool("debug-ai", false, "print sanitized raw model response when strict JSON parsing fails")
	allowlistModeValue := flag.String("allowlist-mode", "off", "allowlist mode: off|warn|enforce")
	allowlistFile := flag.String("allowlist-file", ".smartsh-allowlist", "path to allowlist file")
	flag.Parse()

	userInput := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if *agentMode {
		*jsonMode = true
		*autoConfirm = true
		requestInput, agentRequestData, requestError := resolveAgentInput(userInput)
		if requestError != nil {
			return fail(runResult{
				Executed: false,
				ExitCode: exitFailure,
				Error:    fmt.Sprintf("agent request parse failed: %v", requestError),
			}, true)
		}
		userInput = requestInput
		applyAgentOptions(agentRequestData, unsafeExecution, autoConfirm, dryRun, allowlistModeValue, allowlistFile, explicitWorkingDirectory)
	}

	if strings.TrimSpace(*explicitWorkingDirectory) != "" {
		if chdirError := os.Chdir(strings.TrimSpace(*explicitWorkingDirectory)); chdirError != nil {
			return fail(runResult{
				Executed: false,
				ExitCode: exitFailure,
				Error:    fmt.Sprintf("failed to change directory: %v", chdirError),
			}, *jsonMode)
		}
	}

	if userInput == "" {
		return fail(runResult{
			Executed: false,
			ExitCode: exitFailure,
			Error:    "usage: smartsh [--unsafe] [--yes] [--json] [--dry-run] [--agent] [--cwd path] [--debug-ai] [--allowlist-mode off|warn|enforce] [--allowlist-file path] run this project",
		}, *jsonMode)
	}

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	allowlistMode, allowlistModeError := security.ParseAllowlistMode(*allowlistModeValue)
	if allowlistModeError != nil {
		return fail(runResult{
			Executed: false,
			ExitCode: exitFailure,
			Error:    allowlistModeError.Error(),
		}, *jsonMode)
	}

	var commandAllowlist *security.Allowlist
	if allowlistMode != security.AllowlistModeOff {
		loadedAllowlist, loadAllowlistError := security.LoadAllowlist(*allowlistFile)
		if loadAllowlistError != nil {
			return fail(runResult{
				Executed: false,
				ExitCode: exitFailure,
				Error:    fmt.Sprintf("allowlist load failed: %v", loadAllowlistError),
			}, *jsonMode)
		}
		commandAllowlist = loadedAllowlist
	}

	environment, detectionError := detector.DetectEnvironment()
	if detectionError != nil {
		return fail(runResult{
			Executed: false,
			ExitCode: exitFailure,
			Error:    fmt.Sprintf("environment detection failed: %v", detectionError),
		}, *jsonMode)
	}

	aiClient := ai.NewClientFromEnv()
	aiResponse, aiError := aiClient.GenerateIntent(ctx, userInput, environment)
	if aiError != nil {
		if *debugAI {
			if rawModelResponse, hasRawModelResponse := ai.DebugRawResponseFromError(aiError); hasRawModelResponse {
				fmt.Fprintf(os.Stderr, "debug-ai raw model response: %s\n", rawModelResponse)
			}
		}
		return fail(runResult{
			Executed: false,
			ExitCode: exitFailure,
			Error:    fmt.Sprintf("ai resolution failed: %v", aiError),
		}, *jsonMode)
	}

	resolvedCommand := resolver.ResolveCommand(aiResponse, environment)
	resolvedCommand = resolver.NormalizeCommand(resolvedCommand, environment)
	if resolvedCommand == "" {
		return fail(runResult{
			Executed:        false,
			ResolvedCommand: "",
			ExitCode:        exitFailure,
			Error:           "unable to resolve a command from AI output",
		}, *jsonMode)
	}

	commandAssessment, validationError := security.AssessCommand(resolvedCommand, strings.ToLower(aiResponse.Risk), *unsafeExecution)
	if validationError != nil {
		return fail(runResult{
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitBlocked,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			BlockedReason:   validationError.Error(),
			Error:           "command blocked by safety policy",
		}, *jsonMode)
	}

	allowlistWarning, allowlistValidationError := security.ValidateAllowlist(resolvedCommand, commandAllowlist, allowlistMode)
	if allowlistValidationError != nil {
		return fail(runResult{
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitBlocked,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			BlockedReason:   allowlistValidationError.Error(),
			Error:           "command blocked by allowlist policy",
		}, *jsonMode)
	}

	if !*jsonMode {
		fmt.Printf("Intent: %s\n", aiResponse.Intent)
		fmt.Printf("Confidence: %s\n", ai.FormatConfidence(aiResponse.Confidence))
		fmt.Printf("Risk: %s\n", aiResponse.Risk)
		fmt.Printf("Resolved command: %s\n", resolvedCommand)
		if allowlistWarning != "" {
			fmt.Println(allowlistWarning)
		}
	}

	if *dryRun {
		result := runResult{
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitSuccess,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			AllowlistNotice: allowlistWarning,
		}
		if *jsonMode {
			printJSON(result)
		} else {
			fmt.Println("Dry run enabled: command was not executed.")
		}
		return exitSuccess
	}

	if commandAssessment.RequiresRiskConfirmation && !*unsafeExecution {
		confirmedRiskyCommand, riskyCommandConfirmationError := executor.ConfirmRiskyExecution(resolvedCommand, commandAssessment.RiskReason, true)
		if riskyCommandConfirmationError != nil {
			return fail(runResult{
				Executed:        false,
				ResolvedCommand: resolvedCommand,
				ExitCode:        exitFailure,
				Intent:          aiResponse.Intent,
				Confidence:      ai.FormatConfidence(aiResponse.Confidence),
				Risk:            aiResponse.Risk,
				Error:           fmt.Sprintf("risky confirmation failed: %v", riskyCommandConfirmationError),
			}, *jsonMode)
		}
		if !confirmedRiskyCommand {
			return fail(runResult{
				Executed:        false,
				ResolvedCommand: resolvedCommand,
				ExitCode:        exitFailure,
				Intent:          aiResponse.Intent,
				Confidence:      ai.FormatConfidence(aiResponse.Confidence),
				Risk:            aiResponse.Risk,
				Error:           "risky command cancelled by user",
			}, *jsonMode)
		}
	}

	shouldAutoConfirm := *autoConfirm || *jsonMode
	confirmed, confirmationError := executor.ConfirmExecution(resolvedCommand, shouldAutoConfirm)
	if confirmationError != nil {
		return fail(runResult{
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitFailure,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			Error:           fmt.Sprintf("confirmation failed: %v", confirmationError),
		}, *jsonMode)
	}
	if !confirmed {
		return fail(runResult{
			Executed:        false,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitFailure,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			Error:           "execution cancelled by user",
		}, *jsonMode)
	}

	exitCode, executionError := executor.RunStreaming(ctx, resolvedCommand)
	if executionError != nil {
		if errors.Is(executionError, context.Canceled) {
			return fail(runResult{
				Executed:        false,
				ResolvedCommand: resolvedCommand,
				ExitCode:        exitInterrupted,
				Intent:          aiResponse.Intent,
				Confidence:      ai.FormatConfidence(aiResponse.Confidence),
				Risk:            aiResponse.Risk,
				Error:           "execution interrupted",
			}, *jsonMode)
		}
		return fail(runResult{
			Executed:        true,
			ResolvedCommand: resolvedCommand,
			ExitCode:        exitCode,
			Intent:          aiResponse.Intent,
			Confidence:      ai.FormatConfidence(aiResponse.Confidence),
			Risk:            aiResponse.Risk,
			Error:           fmt.Sprintf("execution failed: %v", executionError),
		}, *jsonMode)
	}

	result := runResult{
		Executed:        true,
		ResolvedCommand: resolvedCommand,
		ExitCode:        exitCode,
		Intent:          aiResponse.Intent,
		Confidence:      ai.FormatConfidence(aiResponse.Confidence),
		Risk:            aiResponse.Risk,
		AllowlistNotice: allowlistWarning,
	}
	if *jsonMode {
		printJSON(result)
	}
	return exitCode
}

func fail(result runResult, jsonMode bool) int {
	if jsonMode {
		printJSON(result)
	} else if result.Error != "" {
		fmt.Fprintln(os.Stderr, result.Error)
	}
	return result.ExitCode
}

func printJSON(result runResult) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if encodeError := encoder.Encode(result); encodeError != nil {
		fmt.Fprintf(os.Stderr, "{\"executed\":false,\"exit_code\":1,\"error\":\"json encode error: %v\"}\n", encodeError)
	}
}

func resolveAgentInput(commandLineInput string) (string, agentRequest, error) {
	trimmedCommandLineInput := strings.TrimSpace(commandLineInput)
	if trimmedCommandLineInput != "" {
		return trimmedCommandLineInput, agentRequest{}, nil
	}

	stdinBytes, readError := io.ReadAll(os.Stdin)
	if readError != nil {
		return "", agentRequest{}, readError
	}
	rawInput := strings.TrimSpace(string(stdinBytes))
	if rawInput == "" {
		return "", agentRequest{}, nil
	}

	if strings.HasPrefix(rawInput, "{") {
		requestData := agentRequest{}
		if unmarshalError := json.Unmarshal([]byte(rawInput), &requestData); unmarshalError != nil {
			return "", agentRequest{}, unmarshalError
		}
		return strings.TrimSpace(requestData.Instruction), requestData, nil
	}

	return rawInput, agentRequest{}, nil
}

func applyAgentOptions(requestData agentRequest, unsafeExecution *bool, autoConfirm *bool, dryRun *bool, allowlistModeValue *string, allowlistFile *string, explicitWorkingDirectory *string) {
	if requestData.Unsafe != nil {
		*unsafeExecution = *requestData.Unsafe
	}
	if requestData.Yes != nil {
		*autoConfirm = *requestData.Yes
	}
	if requestData.DryRun != nil {
		*dryRun = *requestData.DryRun
	}
	if strings.TrimSpace(requestData.AllowlistMode) != "" {
		*allowlistModeValue = strings.TrimSpace(requestData.AllowlistMode)
	}
	if strings.TrimSpace(requestData.AllowlistFile) != "" {
		*allowlistFile = strings.TrimSpace(requestData.AllowlistFile)
	}
	if strings.TrimSpace(requestData.Cwd) != "" {
		*explicitWorkingDirectory = strings.TrimSpace(requestData.Cwd)
	}
}
