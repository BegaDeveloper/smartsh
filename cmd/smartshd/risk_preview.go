package main

import (
	"path/filepath"
	"strings"
)

var riskyInstructionTokens = []string{
	"delete",
	"remove",
	"wipe",
	"reset",
	"prune",
	"drop",
	"destroy",
}

func shouldDryRunFirst(instruction string) bool {
	loweredInstruction := strings.ToLower(strings.TrimSpace(instruction))
	if loweredInstruction == "" {
		return false
	}
	for _, token := range riskyInstructionTokens {
		if strings.Contains(loweredInstruction, token) {
			return true
		}
	}
	return false
}

func extractRiskTargets(command string, cwd string) []string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil
	}

	targets := make([]string, 0)
	loweredCommand := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(loweredCommand, "rm "):
		targets = append(targets, extractDeleteTargets(fields, cwd)...)
	case strings.HasPrefix(loweredCommand, "del "), strings.HasPrefix(loweredCommand, "erase "):
		targets = append(targets, extractDeleteTargets(fields, cwd)...)
	case strings.Contains(loweredCommand, "git clean"):
		targets = append(targets, "untracked files in repository")
	case strings.Contains(loweredCommand, "git reset --hard"):
		targets = append(targets, "all tracked local git changes")
	case strings.Contains(loweredCommand, "docker system prune"):
		targets = append(targets, "unused docker images/containers/volumes")
	case strings.Contains(loweredCommand, "docker compose down -v"):
		targets = append(targets, "docker compose services and attached volumes")
	}
	return uniqueStrings(targets)
}

func extractDeleteTargets(fields []string, cwd string) []string {
	targets := make([]string, 0)
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") {
			continue
		}
		trimmed := strings.Trim(strings.TrimSpace(field), `"'`)
		if trimmed == "" || trimmed == "." {
			continue
		}
		if filepath.IsAbs(trimmed) {
			targets = append(targets, trimmed)
			continue
		}
		targets = append(targets, filepath.Join(cwd, trimmed))
	}
	return targets
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	unique := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		unique = append(unique, trimmed)
	}
	return unique
}
