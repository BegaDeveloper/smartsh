package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type summaryProviderResult struct {
	Summary parsedSummary
	Source  string
}

func resolveSummary(command string, exitCode int, output string, runErr error, client *http.Client) summaryProviderResult {
	deterministic := deterministicSummary(command, exitCode, output, runErr)
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("SMARTSH_SUMMARY_PROVIDER")))
	if provider == "" {
		provider = "deterministic"
	}
	switch provider {
	case "deterministic":
		return summaryProviderResult{Summary: deterministic, Source: "deterministic"}
	case "ollama":
		ollamaSummary, ok := ollamaSummaryForOutput(command, exitCode, output, deterministic, client)
		if ok {
			return summaryProviderResult{Summary: ollamaSummary, Source: "ollama"}
		}
		return summaryProviderResult{Summary: deterministic, Source: "deterministic"}
	case "hybrid":
		if shouldUseOllamaFallback(deterministic, exitCode) {
			ollamaSummary, ok := ollamaSummaryForOutput(command, exitCode, output, deterministic, client)
			if ok {
				return summaryProviderResult{Summary: ollamaSummary, Source: "hybrid_ollama"}
			}
		}
		return summaryProviderResult{Summary: deterministic, Source: "deterministic"}
	default:
		return summaryProviderResult{Summary: deterministic, Source: "deterministic"}
	}
}

func shouldUseOllamaFallback(summary parsedSummary, exitCode int) bool {
	if exitCode == 0 {
		return false
	}
	if strings.TrimSpace(summary.PrimaryError) == "" {
		return true
	}
	if strings.TrimSpace(summary.ErrorType) == "" || strings.EqualFold(summary.ErrorType, "runtime") {
		return true
	}
	return false
}

func ollamaSummaryForOutput(command string, exitCode int, output string, deterministic parsedSummary, client *http.Client) (parsedSummary, bool) {
	url := strings.TrimSpace(os.Getenv("SMARTSH_OLLAMA_URL"))
	if url == "" {
		url = "http://127.0.0.1:11434"
	}
	model := strings.TrimSpace(os.Getenv("SMARTSH_OLLAMA_MODEL"))
	if model == "" {
		model = "llama3.2:3b"
	}
	maxChars := parsePositiveIntEnv("SMARTSH_OLLAMA_MAX_INPUT_CHARS", 3500)
	timeoutSec := parsePositiveIntEnv("SMARTSH_OLLAMA_TIMEOUT_SEC", 8)
	boundedOutput := tailString(output, maxChars)
	redactedOutput := redactForModel(boundedOutput)
	prompt := buildOllamaPrompt(command, exitCode, redactedOutput)

	requestBody := map[string]any{
		"model":  model,
		"stream": false,
		"prompt": prompt,
		"options": map[string]any{
			"temperature": 0,
		},
	}
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return deterministic, false
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(url, "/")+"/api/generate", bytes.NewReader(payload))
	if err != nil {
		return deterministic, false
	}
	request.Header.Set("Content-Type", "application/json")
	ollamaClient := client
	if ollamaClient == nil {
		ollamaClient = &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	} else {
		ollamaClient = &http.Client{Timeout: time.Duration(timeoutSec) * time.Second, Transport: ollamaClient.Transport}
	}
	response, err := ollamaClient.Do(request)
	if err != nil {
		return deterministic, false
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		return deterministic, false
	}
	rawBody, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	if err != nil {
		return deterministic, false
	}
	type ollamaResponse struct {
		Response string `json:"response"`
	}
	parsed := ollamaResponse{}
	if err := json.Unmarshal(rawBody, &parsed); err != nil {
		return deterministic, false
	}
	normalized, ok := parseOllamaSummaryJSON(parsed.Response)
	if !ok {
		return deterministic, false
	}
	return mergeSummary(deterministic, normalized), true
}

func buildOllamaPrompt(command string, exitCode int, outputTail string) string {
	return "You are summarizing terminal failures for an AI coding agent.\n" +
		"Return ONLY compact JSON with keys: summary,error_type,primary_error,next_action,failed_files.\n" +
		"error_type must be one of: none,compile,test,dependency,runtime,policy.\n" +
		"failed_files must be an array of file path strings (or empty array).\n" +
		"Do not include markdown.\n\n" +
		"command: " + command + "\n" +
		"exit_code: " + strconv.Itoa(exitCode) + "\n" +
		"output_tail:\n" + outputTail + "\n"
}

func parseOllamaSummaryJSON(text string) (parsedSummary, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return parsedSummary{}, false
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return parsedSummary{}, false
	}
	fragment := trimmed[start : end+1]
	raw := struct {
		Summary      string   `json:"summary"`
		ErrorType    string   `json:"error_type"`
		PrimaryError string   `json:"primary_error"`
		NextAction   string   `json:"next_action"`
		FailedFiles  []string `json:"failed_files"`
	}{}
	if err := json.Unmarshal([]byte(fragment), &raw); err != nil {
		return parsedSummary{}, false
	}
	if strings.TrimSpace(raw.Summary) == "" {
		return parsedSummary{}, false
	}
	return parsedSummary{
		Summary:      strings.TrimSpace(raw.Summary),
		ErrorType:    strings.TrimSpace(raw.ErrorType),
		PrimaryError: strings.TrimSpace(raw.PrimaryError),
		NextAction:   strings.TrimSpace(raw.NextAction),
		FailedFiles:  raw.FailedFiles,
	}, true
}

func mergeSummary(base parsedSummary, ollama parsedSummary) parsedSummary {
	merged := base
	if strings.TrimSpace(ollama.Summary) != "" {
		merged.Summary = ollama.Summary
	}
	if strings.TrimSpace(ollama.ErrorType) != "" {
		merged.ErrorType = ollama.ErrorType
	}
	if strings.TrimSpace(ollama.PrimaryError) != "" {
		merged.PrimaryError = ollama.PrimaryError
	}
	if strings.TrimSpace(ollama.NextAction) != "" {
		merged.NextAction = ollama.NextAction
	}
	if len(ollama.FailedFiles) > 0 {
		merged.FailedFiles = ollama.FailedFiles
	}
	return merged
}

func parsePositiveIntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func redactForModel(input string) string {
	redacted := input
	patterns := []struct {
		re          *regexp.Regexp
		replacement string
	}{
		{re: regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[A-Za-z0-9\-\._~\+\/]+=*`), replacement: "${1}[REDACTED]"},
		{re: regexp.MustCompile(`(?i)(api[_-]?key\s*[:=]\s*)["']?[A-Za-z0-9\-\._]{12,}["']?`), replacement: "${1}[REDACTED]"},
		{re: regexp.MustCompile(`(?i)(token\s*[:=]\s*)["']?[A-Za-z0-9\-\._]{12,}["']?`), replacement: "${1}[REDACTED]"},
		{re: regexp.MustCompile(`-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]*?-----END [A-Z ]+PRIVATE KEY-----`), replacement: "[REDACTED_PRIVATE_KEY]"},
	}
	for _, pattern := range patterns {
		redacted = pattern.re.ReplaceAllString(redacted, pattern.replacement)
	}
	return redacted
}
