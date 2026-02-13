package main

import (
	"fmt"
	"regexp"
	"strings"
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
