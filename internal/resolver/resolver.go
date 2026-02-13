package resolver

import (
	"sort"
	"strings"

	"github.com/BegaDeveloper/smartsh/internal/ai"
	"github.com/BegaDeveloper/smartsh/internal/detector"
)

func ResolveCommand(aiResponse ai.Response, environment detector.Environment) string {
	command := strings.TrimSpace(aiResponse.Command)
	if command != "" {
		return command
	}

	return fallbackCommand(aiResponse.Intent, environment)
}

func fallbackCommand(intent string, environment detector.Environment) string {
	normalizedIntent := strings.ToLower(strings.TrimSpace(intent))
	if normalizedIntent == "" {
		return ""
	}

	switch {
	case strings.Contains(normalizedIntent, "run") || strings.Contains(normalizedIntent, "start"):
		return resolveRunCommand(normalizedIntent, environment)
	case strings.Contains(normalizedIntent, "test"):
		return resolveTestCommand(normalizedIntent, environment)
	case strings.Contains(normalizedIntent, "build"):
		return resolveBuildCommand(normalizedIntent, environment)
	default:
		return ""
	}
}

func resolveRunCommand(intent string, environment detector.Environment) string {
	if nxCommand := resolveNxTargetCommand(intent, environment, []string{"serve", "run", "start", "dev"}); nxCommand != "" {
		return nxCommand
	}

	switch environment.ProjectType {
	case "node":
		if scriptCommand := resolveNodeScriptCommand(environment, []string{"dev", "start"}); scriptCommand != "" {
			return scriptCommand
		}
		if packageManagerRun := resolveNodePackageManagerCommand(environment, "run dev"); packageManagerRun != "" {
			return packageManagerRun
		}
		if environment.Runtimes["npm"] {
			return "npm run dev"
		}
	case "go":
		if environment.Runtimes["go"] {
			return "go run ."
		}
	case "python":
		if environment.Runtimes["python"] {
			return "python3 main.py"
		}
	case "dotnet":
		if environment.Runtimes["dotnet"] {
			return "dotnet run"
		}
	case "java":
		if environment.Runtimes["mvn"] {
			return "mvn spring-boot:run"
		}
		if environment.Runtimes["gradle"] {
			return "gradle run"
		}
	case "docker":
		if environment.Runtimes["docker"] {
			return "docker compose up"
		}
	case "c_cpp":
		if environment.Runtimes["make"] {
			return "make run"
		}
	case "rust":
		return "cargo run"
	}
	return ""
}

func resolveTestCommand(intent string, environment detector.Environment) string {
	if nxCommand := resolveNxTargetCommand(intent, environment, []string{"test", "e2e"}); nxCommand != "" {
		return nxCommand
	}

	switch environment.ProjectType {
	case "node":
		if scriptCommand := resolveNodeScriptCommand(environment, []string{"test", "test:unit", "test:e2e"}); scriptCommand != "" {
			return scriptCommand
		}
		if packageManagerTest := resolveNodePackageManagerCommand(environment, "test"); packageManagerTest != "" {
			return packageManagerTest
		}
		return "npm test"
	case "go":
		return "go test ./..."
	case "python":
		return "python3 -m pytest"
	case "dotnet":
		return "dotnet test"
	case "java":
		if environment.Runtimes["mvn"] {
			return "mvn test"
		}
		if environment.Runtimes["gradle"] {
			return "gradle test"
		}
	case "c_cpp":
		if environment.Runtimes["make"] {
			return "make test"
		}
	case "rust":
		return "cargo test"
	}
	return ""
}

func resolveBuildCommand(intent string, environment detector.Environment) string {
	if nxCommand := resolveNxTargetCommand(intent, environment, []string{"build", "package"}); nxCommand != "" {
		return nxCommand
	}

	switch environment.ProjectType {
	case "node":
		if scriptCommand := resolveNodeScriptCommand(environment, []string{"build"}); scriptCommand != "" {
			return scriptCommand
		}
		if packageManagerBuild := resolveNodePackageManagerCommand(environment, "run build"); packageManagerBuild != "" {
			return packageManagerBuild
		}
		return "npm run build"
	case "go":
		return "go build ./..."
	case "python":
		return "python3 -m build"
	case "dotnet":
		return "dotnet build"
	case "java":
		if environment.Runtimes["mvn"] {
			return "mvn package"
		}
		if environment.Runtimes["gradle"] {
			return "gradle build"
		}
	case "docker":
		return "docker compose build"
	case "c_cpp":
		if environment.Runtimes["make"] {
			return "make"
		}
	case "rust":
		return "cargo build"
	}
	return ""
}

func resolveNodePackageManagerCommand(environment detector.Environment, subcommand string) string {
	switch strings.ToLower(environment.PackageManager) {
	case "pnpm":
		if environment.Runtimes["pnpm"] {
			return "pnpm " + subcommand
		}
	case "yarn":
		if environment.Runtimes["yarn"] {
			if subcommand == "run dev" {
				return "yarn dev"
			}
			if subcommand == "run build" {
				return "yarn build"
			}
			return "yarn " + subcommand
		}
	case "npm":
		if environment.Runtimes["npm"] {
			return "npm " + subcommand
		}
	}
	return ""
}

func resolveNodeScriptCommand(environment detector.Environment, preferredScripts []string) string {
	if len(environment.NodeScripts) == 0 {
		return ""
	}
	for _, scriptName := range preferredScripts {
		if _, exists := environment.NodeScripts[scriptName]; !exists {
			continue
		}
		switch strings.ToLower(environment.PackageManager) {
		case "pnpm":
			if environment.Runtimes["pnpm"] {
				return "pnpm run " + scriptName
			}
		case "yarn":
			if environment.Runtimes["yarn"] {
				return "yarn " + scriptName
			}
		default:
			if environment.Runtimes["npm"] {
				if scriptName == "start" {
					return "npm start"
				}
				return "npm run " + scriptName
			}
		}
	}
	return ""
}

func resolveNxTargetCommand(intent string, environment detector.Environment, preferredTargets []string) string {
	if environment.WorkspaceKind != "nx" || len(environment.NxTargets) == 0 {
		return ""
	}

	projectName := resolveNxProjectFromIntent(intent, environment.NxTargets)
	if projectName == "" {
		projectName = firstSortedProject(environment.NxTargets)
	}
	if projectName == "" {
		return ""
	}

	projectTargets := environment.NxTargets[projectName]
	targetSet := map[string]bool{}
	for _, target := range projectTargets {
		targetSet[target] = true
	}

	selectedTarget := ""
	for _, target := range preferredTargets {
		if targetSet[target] {
			selectedTarget = target
			break
		}
	}
	if selectedTarget == "" {
		return ""
	}

	switch strings.ToLower(environment.PackageManager) {
	case "pnpm":
		if environment.Runtimes["pnpm"] {
			return "pnpm nx " + selectedTarget + " " + projectName
		}
	case "yarn":
		if environment.Runtimes["yarn"] {
			return "yarn nx " + selectedTarget + " " + projectName
		}
	default:
		if environment.Runtimes["npm"] {
			return "npx nx " + selectedTarget + " " + projectName
		}
	}
	return ""
}

func resolveNxProjectFromIntent(intent string, targets map[string][]string) string {
	normalizedIntent := strings.ToLower(strings.TrimSpace(intent))
	if normalizedIntent == "" {
		return ""
	}
	sortedProjects := make([]string, 0, len(targets))
	for projectName := range targets {
		sortedProjects = append(sortedProjects, projectName)
	}
	sort.Strings(sortedProjects)
	for _, projectName := range sortedProjects {
		if strings.Contains(normalizedIntent, strings.ToLower(projectName)) {
			return projectName
		}
	}
	return ""
}

func firstSortedProject(targets map[string][]string) string {
	if len(targets) == 0 {
		return ""
	}
	projectNames := make([]string, 0, len(targets))
	for projectName := range targets {
		projectNames = append(projectNames, projectName)
	}
	sort.Strings(projectNames)
	return projectNames[0]
}
