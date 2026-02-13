package resolver

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/BegaDeveloper/smartsh/internal/ai"
	"github.com/BegaDeveloper/smartsh/internal/detector"
)

var gitLastCommitsPattern = regexp.MustCompile(`(?i)(last|recent)\s+(\d+)\s+commits?`)

func ResolveDeterministicIntent(userInput string, environment detector.Environment) (ai.Response, bool) {
	normalizedInput := strings.ToLower(strings.TrimSpace(userInput))
	if normalizedInput == "" {
		return ai.Response{}, false
	}

	if environment.Runtimes["git"] {
		if command, ok := resolveGitInspectCommand(normalizedInput); ok {
			return ai.Response{
				Intent:     "inspect",
				Command:    command,
				Confidence: 0.99,
				Risk:       "low",
			}, true
		}
	}

	return ai.Response{}, false
}

func resolveGitInspectCommand(normalizedInput string) (string, bool) {
	matches := gitLastCommitsPattern.FindStringSubmatch(normalizedInput)
	if len(matches) == 3 {
		commitCount, parseError := strconv.Atoi(matches[2])
		if parseError == nil {
			commitCount = clamp(commitCount, 1, 50)
			return "git log --oneline -n " + strconv.Itoa(commitCount), true
		}
	}
	if strings.Contains(normalizedInput, "last commits") || strings.Contains(normalizedInput, "recent commits") {
		return "git log --oneline -n 5", true
	}
	if strings.Contains(normalizedInput, "git status") || strings.Contains(normalizedInput, "status of repo") {
		return "git status -sb", true
	}
	return "", false
}

func clamp(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
