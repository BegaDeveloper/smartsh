package security

import (
	"fmt"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type CommandAssessment struct {
	RequiresRiskConfirmation bool
	RiskLevel                string
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
			assessment.RiskLevel = maxRiskLevel(assessment.RiskLevel, suspiciousPattern.riskLevel)
			assessment.RiskReason = suspiciousPattern.reason
			break
		}
	}

	if astRiskReason, astRiskLevel := detectASTRisk(normalizedCommand); astRiskReason != "" {
		assessment.RequiresRiskConfirmation = true
		assessment.RiskLevel = maxRiskLevel(assessment.RiskLevel, astRiskLevel)
		if assessment.RiskReason == "" {
			assessment.RiskReason = astRiskReason
		}
	}

	if strings.EqualFold(risk, "high") {
		assessment.RequiresRiskConfirmation = true
		assessment.RiskLevel = maxRiskLevel(assessment.RiskLevel, "high")
		if assessment.RiskReason == "" {
			assessment.RiskReason = "model marked command as high risk"
		}
	} else if strings.EqualFold(risk, "medium") {
		assessment.RequiresRiskConfirmation = true
		assessment.RiskLevel = maxRiskLevel(assessment.RiskLevel, "medium")
		if assessment.RiskReason == "" {
			assessment.RiskReason = "model marked command as medium risk"
		}
	}
	if assessment.RiskLevel == "" {
		assessment.RiskLevel = "low"
	}

	return assessment, nil
}

func detectASTRisk(command string) (string, string) {
	parser := syntax.NewParser()
	file, parseError := parser.Parse(strings.NewReader(command), "")
	if parseError != nil {
		return "", ""
	}

	riskReason := ""
	riskLevel := "low"
	syntax.Walk(file, func(node syntax.Node) bool {
		switch typedNode := node.(type) {
		case *syntax.Redirect:
			if riskReason == "" {
				riskReason = "shell redirection detected"
			}
			riskLevel = maxRiskLevel(riskLevel, "medium")
		case *syntax.Subshell:
			if riskReason == "" {
				riskReason = "subshell command detected"
			}
			riskLevel = maxRiskLevel(riskLevel, "medium")
		case *syntax.CmdSubst:
			if riskReason == "" {
				riskReason = "command substitution detected"
			}
			riskLevel = maxRiskLevel(riskLevel, "medium")
		case *syntax.BinaryCmd:
			operatorText := typedNode.Op.String()
			if strings.Contains(operatorText, "|") {
				if riskReason == "" {
					riskReason = "pipeline command detected"
				}
				riskLevel = maxRiskLevel(riskLevel, "medium")
			}
		}
		return true
	})

	return riskReason, riskLevel
}

func maxRiskLevel(left string, right string) string {
	if riskLevelRank(right) > riskLevelRank(left) {
		return right
	}
	return left
}

func riskLevelRank(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
