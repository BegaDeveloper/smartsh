package main

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type parsedSummary struct {
	Summary      string
	ErrorType    string
	PrimaryError string
	NextAction   string
	FailingTests []string
	FailedFiles  []string
	TopIssues    []string
}

func deterministicSummary(command string, exitCode int, output string, runErr error) parsedSummary {
	if exitCode == 0 && runErr == nil {
		return parsedSummary{Summary: "command completed successfully", ErrorType: "none"}
	}
	lines := splitNonEmptyLines(output)
	issueLines := pickIssueLines(lines, 5)

	summary := parsedSummary{
		Summary:   fmt.Sprintf("command failed (exit code %d)", exitCode),
		ErrorType: classifyErrorType(command, output, runErr, exitCode),
		TopIssues: issueLines[:min(len(issueLines), 3)],
	}
	if len(issueLines) > 0 {
		summary.PrimaryError = issueLines[0]
		summary.Summary = fmt.Sprintf("command failed (exit code %d): %s", exitCode, issueLines[0])
	}

	switch {
	case parseGoTest(lines, &summary):
		return summary
	case parseJestVitest(lines, &summary):
		return summary
	case parseTypeScript(lines, &summary):
		return summary
	case parseMaven(lines, &summary):
		return summary
	case parseGradle(lines, &summary):
		return summary
	case parseDotNet(lines, &summary):
		return summary
	default:
		return summary
	}
}

func parseJestVitest(lines []string, summary *parsedSummary) bool {
	failSuite := regexp.MustCompile(`(?i)^FAIL\s+(.+)$`)
	failTest := regexp.MustCompile(`(?i)^\s*[●•]\s+(.+)$`)
	matched := false
	for _, line := range lines {
		if m := failSuite.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 2 {
			summary.FailedFiles = appendUnique(summary.FailedFiles, strings.TrimSpace(m[1]), 6)
			matched = true
			continue
		}
		if m := failTest.FindStringSubmatch(line); len(m) == 2 {
			summary.FailingTests = appendUnique(summary.FailingTests, strings.TrimSpace(m[1]), 12)
			matched = true
		}
	}
	if matched {
		summary.ErrorType = "test"
		summary.NextAction = "Fix failing tests and rerun test command."
	}
	return matched
}

func parseGoTest(lines []string, summary *parsedSummary) bool {
	failTest := regexp.MustCompile(`^--- FAIL:\s*([^\s]+)`)
	failFile := regexp.MustCompile(`^FAIL\s+([^\s]+)\s+[\d\.]+s?$`)
	matched := false
	hasGoFailureMarker := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := failTest.FindStringSubmatch(trimmed); len(m) == 2 {
			summary.FailingTests = appendUnique(summary.FailingTests, m[1], 12)
			matched = true
			hasGoFailureMarker = true
		} else if strings.HasPrefix(trimmed, "--- FAIL:") {
			fields := strings.Fields(strings.TrimPrefix(trimmed, "--- FAIL:"))
			if len(fields) > 0 {
				summary.FailingTests = appendUnique(summary.FailingTests, fields[0], 12)
				matched = true
				hasGoFailureMarker = true
			}
		}
		if m := failFile.FindStringSubmatch(trimmed); len(m) == 2 {
			summary.FailedFiles = appendUnique(summary.FailedFiles, m[1], 6)
			hasGoFailureMarker = true
		}
	}
	if hasGoFailureMarker {
		matched = true
		summary.ErrorType = "test"
		summary.NextAction = "Fix failing go tests and rerun go test."
	}
	return matched
}

func parseTypeScript(lines []string, summary *parsedSummary) bool {
	re := regexp.MustCompile(`(?i)^(.+\.(ts|tsx))\((\d+),(\d+)\):\s*error\s*(TS\d+):\s*(.+)$`)
	matched := false
	for _, line := range lines {
		if m := re.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 7 {
			summary.FailedFiles = appendUnique(summary.FailedFiles, m[1], 6)
			if summary.PrimaryError == "" {
				summary.PrimaryError = m[5] + " " + m[6]
			}
			matched = true
		}
	}
	if matched {
		summary.ErrorType = "compile"
		summary.NextAction = "Fix TypeScript compiler errors and rerun build/test."
	}
	return matched
}

func parseMaven(lines []string, summary *parsedSummary) bool {
	matched := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "[ERROR] COMPILATION ERROR") || strings.Contains(trimmed, "Failed to execute goal") {
			matched = true
		}
	}
	if matched {
		summary.ErrorType = "compile"
		summary.NextAction = "Fix Maven compilation/build errors and rerun mvn test/build."
	}
	return matched
}

func parseGradle(lines []string, summary *parsedSummary) bool {
	matched := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "Execution failed for task") || strings.Contains(trimmed, "BUILD FAILED") {
			matched = true
		}
	}
	if matched {
		summary.ErrorType = "compile"
		summary.NextAction = "Fix Gradle task/build failures and rerun gradle build/test."
	}
	return matched
}

func parseDotNet(lines []string, summary *parsedSummary) bool {
	re := regexp.MustCompile(`(?i)^(.+\.(cs|fs|vb))\((\d+),(\d+)\):\s*error\s+([A-Z]+\d+):\s+(.+)$`)
	matched := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if m := re.FindStringSubmatch(trimmed); len(m) == 7 {
			summary.FailedFiles = appendUnique(summary.FailedFiles, m[1], 6)
			if summary.PrimaryError == "" {
				summary.PrimaryError = m[5] + " " + m[6]
			}
			matched = true
			continue
		}
		if strings.Contains(trimmed, "Test Run Failed.") || strings.Contains(trimmed, "Failed!") {
			summary.ErrorType = "test"
			summary.NextAction = "Fix .NET test failures and rerun dotnet test."
			matched = true
		}
	}
	if matched && summary.ErrorType == "" {
		summary.ErrorType = "compile"
		summary.NextAction = "Fix .NET compile errors and rerun dotnet build/test."
	}
	return matched
}

func pickIssueLines(lines []string, max int) []string {
	if max <= 0 {
		return nil
	}
	errorMatcher := regexp.MustCompile(`(?i)(error|exception|panic|failed|fail|TS[0-9]{3,}|ERR!|Cannot find module|BUILD FAILED)`)
	issues := make([]string, 0, max)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if errorMatcher.MatchString(trimmed) {
			issues = append(issues, trimmed)
		}
		if len(issues) >= max {
			break
		}
	}
	return issues
}

func appendUnique(values []string, value string, max int) []string {
	if value == "" {
		return values
	}
	for _, current := range values {
		if current == value {
			return values
		}
	}
	values = append(values, value)
	if len(values) > max {
		return values[:max]
	}
	return values
}

type ollamaSummaryResponse struct {
	Summary      string   `json:"summary"`
	ErrorType    string   `json:"error_type"`
	PrimaryError string   `json:"primary_error"`
	NextAction   string   `json:"next_action"`
	FailingTests []string `json:"failing_tests"`
	FailedFiles  []string `json:"failed_files"`
	TopIssues    []string `json:"top_issues"`
}

func (server *daemonServer) tryOllamaSummary(ctx context.Context, command string, output string, deterministic parsedSummary) (parsedSummary, bool) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SMARTSH_SUMMARY_ENABLED")), "false") {
		return parsedSummary{}, false
	}

	model := strings.TrimSpace(os.Getenv("SMARTSH_SUMMARY_MODEL"))
	if model == "" {
		model = strings.TrimSpace(os.Getenv("SMARTSH_MODEL"))
	}
	if model == "" {
		model = "llama3.1:8b"
	}
	ollamaURL := strings.TrimSpace(os.Getenv("SMARTSH_OLLAMA_URL"))
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434/api/generate"
	}

	outputForPrompt := output
	if len(outputForPrompt) > 9000 {
		outputForPrompt = outputForPrompt[len(outputForPrompt)-9000:]
	}

	prompt := fmt.Sprintf(`Classify and summarize terminal command output.
Return strict JSON only:
{"summary":string,"error_type":"compile|test|runtime|dependency|none","primary_error":string,"next_action":string,"failing_tests":[string],"failed_files":[string],"top_issues":[string]}

Command:
%s

Output:
%s`, command, outputForPrompt)
	debugPrefix := startSummaryDebugSession(command)
	writeSummaryDebugFile(debugPrefix, "01_prompt.txt", prompt)
	payload := map[string]any{
		"model":  model,
		"prompt": prompt,
		"stream": false,
		"format": "json",
		"options": map[string]any{
			"temperature": 0,
		},
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return parsedSummary{}, false
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, ollamaURL, strings.NewReader(string(payloadBytes)))
	if requestErr != nil {
		return parsedSummary{}, false
	}
	request.Header.Set("Content-Type", "application/json")
	response, responseErr := server.httpClient.Do(request)
	if responseErr != nil {
		writeSummaryDebugFile(debugPrefix, "02_http_error.txt", responseErr.Error())
		return parsedSummary{}, false
	}
	defer response.Body.Close()

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil || response.StatusCode < 200 || response.StatusCode > 299 {
		writeSummaryDebugFile(debugPrefix, "02_http_error.txt", fmt.Sprintf("status=%d readErr=%v body=%s", response.StatusCode, readErr, string(body)))
		return parsedSummary{}, false
	}
	writeSummaryDebugFile(debugPrefix, "02_raw_ollama_body.json", string(body))

	outer := struct {
		Response string `json:"response"`
	}{}
	if err := json.Unmarshal(body, &outer); err != nil {
		writeSummaryDebugFile(debugPrefix, "03_parse_error.txt", "outer unmarshal error: "+err.Error())
		return parsedSummary{}, false
	}
	writeSummaryDebugFile(debugPrefix, "03_raw_ollama_response.txt", outer.Response)
	inner := ollamaSummaryResponse{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(outer.Response)), &inner); err != nil {
		writeSummaryDebugFile(debugPrefix, "03_parse_error.txt", "inner unmarshal error: "+err.Error())
		return parsedSummary{}, false
	}
	writeSummaryDebugJSON(debugPrefix, "04_parsed_ollama_summary.json", inner)

	final := deterministic
	if trimmed := strings.TrimSpace(inner.Summary); trimmed != "" {
		final.Summary = trimmed
	}
	errorType := strings.ToLower(strings.TrimSpace(inner.ErrorType))
	if isKnownErrorType(errorType) {
		final.ErrorType = errorType
	}
	if trimmed := strings.TrimSpace(inner.PrimaryError); trimmed != "" {
		final.PrimaryError = trimmed
	}
	if trimmed := strings.TrimSpace(inner.NextAction); trimmed != "" {
		final.NextAction = trimmed
	}
	if len(inner.FailingTests) > 0 {
		final.FailingTests = inner.FailingTests[:min(len(inner.FailingTests), 12)]
	}
	if len(inner.FailedFiles) > 0 {
		final.FailedFiles = inner.FailedFiles[:min(len(inner.FailedFiles), 8)]
	}
	if len(inner.TopIssues) > 0 {
		final.TopIssues = inner.TopIssues[:min(len(inner.TopIssues), 5)]
	}
	writeSummaryDebugJSON(debugPrefix, "05_final_summary_used.json", final)
	return final, true
}

func startSummaryDebugSession(command string) string {
	debugDir := strings.TrimSpace(os.Getenv("SMARTSH_SUMMARY_DEBUG_DIR"))
	if debugDir == "" {
		return ""
	}
	commandHash := sha1.Sum([]byte(command))
	sessionName := fmt.Sprintf("summary-%s-%x", time.Now().UTC().Format("20060102T150405.000Z"), commandHash[:4])
	sessionDir := filepath.Join(debugDir, sessionName)
	if mkdirError := os.MkdirAll(sessionDir, 0o755); mkdirError != nil {
		return ""
	}
	return sessionDir
}

func writeSummaryDebugFile(sessionDir string, fileName string, content string) {
	if strings.TrimSpace(sessionDir) == "" {
		return
	}
	targetPath := filepath.Join(sessionDir, fileName)
	_ = os.WriteFile(targetPath, []byte(content), 0o644)
}

func writeSummaryDebugJSON(sessionDir string, fileName string, payload any) {
	if strings.TrimSpace(sessionDir) == "" {
		return
	}
	encoded, marshalError := json.MarshalIndent(payload, "", "  ")
	if marshalError != nil {
		writeSummaryDebugFile(sessionDir, strings.TrimSuffix(fileName, ".json")+".error.txt", marshalError.Error())
		return
	}
	writeSummaryDebugFile(sessionDir, fileName, string(encoded)+"\n")
}

func isKnownErrorType(errorType string) bool {
	switch errorType {
	case "none", "compile", "test", "runtime", "dependency", "policy":
		return true
	default:
		return false
	}
}
