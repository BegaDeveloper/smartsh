package resolver

import (
	"regexp"
	"strings"

	"github.com/BegaDeveloper/smartsh/internal/detector"
)

var goBuildRootPattern = regexp.MustCompile(`(?i)^\s*go\s+build(\s+[-\w=./]+)*\s+\.\s*$`)
var trailingDotTokenPattern = regexp.MustCompile(`\s+\.\s*$`)

func NormalizeCommand(command string, environment detector.Environment) string {
	normalizedCommand := strings.TrimSpace(command)
	if normalizedCommand == "" {
		return ""
	}

	if environment.ProjectType == "go" && goBuildRootPattern.MatchString(normalizedCommand) {
		return trailingDotTokenPattern.ReplaceAllString(normalizedCommand, " ./...")
	}

	return normalizedCommand
}
