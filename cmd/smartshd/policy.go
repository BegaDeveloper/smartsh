package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type projectPolicy struct {
	Version       int      `yaml:"version"`
	Enforce       bool     `yaml:"enforce"`
	MaxRisk       string   `yaml:"max_risk"`
	AllowCommands []string `yaml:"allow_commands"`
	DenyCommands  []string `yaml:"deny_commands"`
	AllowPaths    []string `yaml:"allow_paths"`
	DenyPaths     []string `yaml:"deny_paths"`
	AllowEnv      []string `yaml:"allow_env"`
	DenyEnv       []string `yaml:"deny_env"`
}

func findPolicyFile(cwd string) string {
	current := cwd
	for {
		candidate := filepath.Join(current, ".smartsh-policy.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return ""
}

func loadPolicy(cwd string) (*projectPolicy, error) {
	path := findPolicyFile(cwd)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	policy := projectPolicy{}
	if err := yaml.Unmarshal(raw, &policy); err != nil {
		return nil, fmt.Errorf("invalid .smartsh-policy.yaml: %w", err)
	}
	return &policy, nil
}

func applyPolicy(policy *projectPolicy, cwd string, resolvedCommand string, risk string) error {
	if policy == nil {
		return nil
	}
	if policy.MaxRisk != "" && riskRank(strings.ToLower(strings.TrimSpace(risk))) > riskRank(strings.ToLower(strings.TrimSpace(policy.MaxRisk))) {
		return fmt.Errorf("blocked by policy: risk %q exceeds max_risk %q", risk, policy.MaxRisk)
	}

	if matchesAnyRule(resolvedCommand, policy.DenyCommands) {
		return errors.New("blocked by policy: command denied")
	}
	if len(policy.AllowCommands) > 0 && !matchesAnyRule(resolvedCommand, policy.AllowCommands) {
		return errors.New("blocked by policy: command not in allow_commands")
	}

	absoluteCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil
	}
	if len(policy.DenyPaths) > 0 && pathMatchesAny(absoluteCWD, policy.DenyPaths) {
		return errors.New("blocked by policy: cwd denied by deny_paths")
	}
	if len(policy.AllowPaths) > 0 && !pathMatchesAny(absoluteCWD, policy.AllowPaths) {
		return errors.New("blocked by policy: cwd not in allow_paths")
	}
	return nil
}

func buildEnvWithPolicy(policy *projectPolicy, request runRequest) []string {
	baseMap := map[string]string{}
	for _, entry := range os.Environ() {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			baseMap[parts[0]] = parts[1]
		}
	}

	allowedSet := map[string]bool{}
	for _, key := range request.AllowedEnv {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			allowedSet[trimmed] = true
		}
	}
	if policy != nil {
		for _, key := range policy.AllowEnv {
			trimmed := strings.TrimSpace(key)
			if trimmed != "" {
				allowedSet[trimmed] = true
			}
		}
		for _, key := range policy.DenyEnv {
			delete(allowedSet, strings.TrimSpace(key))
			delete(baseMap, strings.TrimSpace(key))
		}
	}

	result := make([]string, 0, len(baseMap)+len(request.Env))
	if len(allowedSet) == 0 {
		for key, value := range baseMap {
			result = append(result, key+"="+value)
		}
	} else {
		for key := range allowedSet {
			if value, exists := baseMap[key]; exists {
				result = append(result, key+"="+value)
			}
		}
	}

	for key, value := range request.Env {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		if len(allowedSet) > 0 && !allowedSet[trimmed] {
			continue
		}
		result = append(result, trimmed+"="+value)
	}
	return result
}

func riskRank(risk string) int {
	switch risk {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 2
	}
}

func matchesAnyRule(command string, rules []string) bool {
	for _, rule := range rules {
		trimmed := strings.TrimSpace(rule)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "exact:") && strings.TrimSpace(strings.TrimPrefix(trimmed, "exact:")) == command {
			return true
		}
		if strings.HasPrefix(trimmed, "prefix:") && strings.HasPrefix(command, strings.TrimSpace(strings.TrimPrefix(trimmed, "prefix:"))) {
			return true
		}
		if strings.HasPrefix(trimmed, "re:") {
			pattern := strings.TrimSpace(strings.TrimPrefix(trimmed, "re:"))
			matched, err := regexp.MatchString(pattern, command)
			if err == nil && matched {
				return true
			}
			continue
		}
		if trimmed == command {
			return true
		}
	}
	return false
}

func pathMatchesAny(path string, rules []string) bool {
	for _, rule := range rules {
		normalized := strings.TrimSpace(rule)
		if normalized == "" {
			continue
		}
		absolute, err := filepath.Abs(normalized)
		if err != nil {
			continue
		}
		if strings.HasPrefix(path, absolute) {
			return true
		}
	}
	return false
}
