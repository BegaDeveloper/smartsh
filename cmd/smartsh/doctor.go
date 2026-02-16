package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BegaDeveloper/smartsh/internal/runtimeconfig"
)

type doctorCheck struct {
	name    string
	ok      bool
	details string
}

func runDoctor(output io.Writer, errorOutput io.Writer) error {
	config, err := runtimeconfig.Load("")
	if err != nil {
		return err
	}
	configValues := config.Values

	checks := []doctorCheck{
		checkDaemonToken(configValues),
		checkDaemonHealth(configValues),
		checkOllamaHealthAndModel(configValues),
		checkGeneratedConfigFiles(),
	}

	hasFailure := false
	for _, check := range checks {
		status := "PASS"
		if !check.ok {
			status = "FAIL"
			hasFailure = true
		}
		fmt.Fprintf(output, "[%s] %s: %s\n", status, check.name, check.details)
	}
	if hasFailure {
		fmt.Fprintln(errorOutput, "")
		fmt.Fprintln(errorOutput, "smartsh doctor found configuration issues.")
		fmt.Fprintln(errorOutput, "Fix the failing checks and rerun: smartsh doctor")
		return fmt.Errorf("one or more doctor checks failed")
	}
	fmt.Fprintln(output, "")
	fmt.Fprintln(output, "smartsh doctor passed: daemon auth, ollama, and MCP config look good.")
	return nil
}

func checkDaemonToken(configValues map[string]string) doctorCheck {
	authDisabled := runtimeconfig.ResolveBool("SMARTSH_DAEMON_DISABLE_AUTH", configValues)
	if authDisabled {
		return doctorCheck{
			name:    "daemon auth/token",
			ok:      true,
			details: "auth is disabled via SMARTSH_DAEMON_DISABLE_AUTH=true",
		}
	}
	token := runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues)
	if strings.TrimSpace(token) == "" {
		return doctorCheck{
			name:    "daemon auth/token",
			ok:      false,
			details: "SMARTSH_DAEMON_TOKEN is empty (run smartsh setup-agent)",
		}
	}
	return doctorCheck{
		name:    "daemon auth/token",
		ok:      true,
		details: "token is configured",
	}
}

func checkDaemonHealth(configValues map[string]string) doctorCheck {
	daemonURL := runtimeconfig.ResolveString("SMARTSH_DAEMON_URL", configValues)
	if daemonURL == "" {
		daemonURL = "http://127.0.0.1:8787"
	}
	authDisabled := runtimeconfig.ResolveBool("SMARTSH_DAEMON_DISABLE_AUTH", configValues)
	token := runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues)

	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(daemonURL, "/")+"/health", nil)
	if err != nil {
		return doctorCheck{name: "daemon health", ok: false, details: err.Error()}
	}
	if !authDisabled && strings.TrimSpace(token) != "" {
		request.Header.Set("X-Smartsh-Token", token)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return doctorCheck{
			name:    "daemon health",
			ok:      false,
			details: fmt.Sprintf("cannot reach daemon at %s/health (%v)", daemonURL, err),
		}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return doctorCheck{
			name:    "daemon health",
			ok:      false,
			details: fmt.Sprintf("daemon returned HTTP %d for /health", response.StatusCode),
		}
	}
	return doctorCheck{name: "daemon health", ok: true, details: "daemon is reachable and healthy"}
}

func checkOllamaHealthAndModel(configValues map[string]string) doctorCheck {
	ollamaURL := runtimeconfig.ResolveString("SMARTSH_OLLAMA_URL", configValues)
	if ollamaURL == "" {
		ollamaURL = "http://127.0.0.1:11434"
	}
	ollamaModel := runtimeconfig.ResolveString("SMARTSH_OLLAMA_MODEL", configValues)
	if ollamaModel == "" {
		ollamaModel = "llama3.2:3b"
	}
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(ollamaURL, "/")+"/api/tags", nil)
	if err != nil {
		return doctorCheck{name: "ollama health/model", ok: false, details: err.Error()}
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return doctorCheck{
			name:    "ollama health/model",
			ok:      false,
			details: fmt.Sprintf("cannot reach ollama at %s (%v)", ollamaURL, err),
		}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return doctorCheck{
			name:    "ollama health/model",
			ok:      false,
			details: fmt.Sprintf("ollama returned HTTP %d", response.StatusCode),
		}
	}
	payload := struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}{}
	if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
		return doctorCheck{
			name:    "ollama health/model",
			ok:      false,
			details: fmt.Sprintf("invalid /api/tags response: %v", decodeErr),
		}
	}
	for _, model := range payload.Models {
		if ollamaModelMatches(ollamaModel, model.Name) || ollamaModelMatches(ollamaModel, model.Model) {
			return doctorCheck{
				name:    "ollama health/model",
				ok:      true,
				details: fmt.Sprintf("ollama is reachable and model %q is installed", ollamaModel),
			}
		}
	}
	return doctorCheck{
		name:    "ollama health/model",
		ok:      false,
		details: fmt.Sprintf("ollama is running but model %q is missing (run: ollama pull %s)", ollamaModel, ollamaModel),
	}
}

func checkGeneratedConfigFiles() doctorCheck {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return doctorCheck{name: "mcp config files", ok: false, details: err.Error()}
	}
	baseDir := filepath.Join(homeDir, ".smartsh")
	requiredFiles := []string{
		filepath.Join(baseDir, "cursor-smartsh-mcp.json"),
		filepath.Join(baseDir, "cursor-mcp.json"),
		filepath.Join(baseDir, "claude-smartsh-tool.json"),
		filepath.Join(baseDir, "agent-instructions.txt"),
	}
	for _, path := range requiredFiles {
		info, statErr := os.Stat(path)
		if statErr != nil {
			return doctorCheck{
				name:    "mcp config files",
				ok:      false,
				details: fmt.Sprintf("missing %s (run smartsh setup-agent)", path),
			}
		}
		if info.Size() == 0 {
			return doctorCheck{
				name:    "mcp config files",
				ok:      false,
				details: fmt.Sprintf("empty file %s", path),
			}
		}
	}
	jsonFiles := []string{
		filepath.Join(baseDir, "cursor-smartsh-mcp.json"),
		filepath.Join(baseDir, "cursor-mcp.json"),
		filepath.Join(baseDir, "claude-smartsh-tool.json"),
	}
	for _, path := range jsonFiles {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return doctorCheck{
				name:    "mcp config files",
				ok:      false,
				details: fmt.Sprintf("read failed for %s: %v", path, readErr),
			}
		}
		var payload any
		if unmarshalErr := json.Unmarshal(raw, &payload); unmarshalErr != nil {
			return doctorCheck{
				name:    "mcp config files",
				ok:      false,
				details: fmt.Sprintf("invalid JSON in %s: %v", path, unmarshalErr),
			}
		}
	}
	return doctorCheck{
		name:    "mcp config files",
		ok:      true,
		details: "generated files exist and JSON is valid",
	}
}

func ollamaModelMatches(requested string, candidate string) bool {
	normalizedRequested := strings.ToLower(strings.TrimSpace(requested))
	normalizedCandidate := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(candidate, "library/")))
	if normalizedRequested == "" || normalizedCandidate == "" {
		return false
	}
	if normalizedRequested == normalizedCandidate {
		return true
	}
	if !strings.Contains(normalizedRequested, ":") && strings.HasPrefix(normalizedCandidate, normalizedRequested+":") {
		return true
	}
	return false
}
