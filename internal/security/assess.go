package security

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type CommandAssessment struct {
	RequiresRiskConfirmation bool
	RiskReason               string
}

func AssessCommand(command string, risk string, allowUnsafe bool) (CommandAssessment, error) {
	normalizedCommand := strings.TrimSpace(command)
	if normalizedCommand == "" {
		return CommandAssessment{}, fmt.Errorf("empty command")
	}

	for _, blockedPattern := range blockedPatterns {
		if blockedPattern.regex.MatchString(normalizedCommand) {
			if allowUnsafe {
				return CommandAssessment{}, nil
			}
			return CommandAssessment{}, fmt.Errorf("blocked: %s", blockedPattern.reason)
		}
	}

	assessment := CommandAssessment{}
	for _, suspiciousPattern := range suspiciousPatterns {
		if suspiciousPattern.regex.MatchString(normalizedCommand) {
			assessment.RequiresRiskConfirmation = true
			assessment.RiskReason = suspiciousPattern.reason
			break
		}
	}

	if astRiskReason := detectASTRisk(normalizedCommand); astRiskReason != "" {
		assessment.RequiresRiskConfirmation = true
		if assessment.RiskReason == "" {
			assessment.RiskReason = astRiskReason
		}
	}

	if strings.EqualFold(risk, "high") {
		assessment.RequiresRiskConfirmation = true
		if assessment.RiskReason == "" {
			assessment.RiskReason = "model marked command as high risk"
		}
	}

	return assessment, nil
}

func detectASTRisk(command string) string {
	parser := syntax.NewParser()
	file, parseError := parser.Parse(strings.NewReader(command), "")
	if parseError != nil {
		return ""
	}

	riskReason := ""
	syntax.Walk(file, func(node syntax.Node) bool {
		switch typedNode := node.(type) {
		case *syntax.Redirect:
			if riskReason == "" {
				riskReason = "shell redirection detected"
			}
		case *syntax.Subshell:
			if riskReason == "" {
				riskReason = "subshell command detected"
			}
		case *syntax.CmdSubst:
			if riskReason == "" {
				riskReason = "command substitution detected"
			}
		case *syntax.BinaryCmd:
			operatorText := typedNode.Op.String()
			if strings.Contains(operatorText, "|") {
				if riskReason == "" {
					riskReason = "pipeline command detected"
				}
			}
		}
		return true
	})

	return riskReason
}
