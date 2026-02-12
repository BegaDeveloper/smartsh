package resolver

import (
	"testing"

	"smartsh/internal/ai"
	"smartsh/internal/detector"
)

func TestResolveCommand_UsesAICommandWhenPresent(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType: "go",
		Runtimes:    map[string]bool{"go": true},
	}
	response := ai.Response{
		Intent:  "run tests",
		Command: "go test ./internal/...",
	}

	command := ResolveCommand(response, environment)
	if command != "go test ./internal/..." {
		t.Fatalf("expected direct AI command, got %q", command)
	}
}

func TestResolveCommand_FallbackForRunNode(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType: "node",
		Runtimes:    map[string]bool{"npm": true},
	}
	response := ai.Response{
		Intent: "run this project",
	}

	command := ResolveCommand(response, environment)
	if command != "npm run dev" {
		t.Fatalf("expected npm run dev, got %q", command)
	}
}

func TestResolveCommand_FallbackForGoTest(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType: "go",
		Runtimes:    map[string]bool{"go": true},
	}
	response := ai.Response{
		Intent: "please test everything",
	}

	command := ResolveCommand(response, environment)
	if command != "go test ./..." {
		t.Fatalf("expected go test ./..., got %q", command)
	}
}

func TestResolveCommand_NodeUsesDetectedPackageManager(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType:    "node",
		PackageManager: "pnpm",
		Runtimes:       map[string]bool{"pnpm": true},
	}
	response := ai.Response{
		Intent: "build project",
	}

	command := ResolveCommand(response, environment)
	if command != "pnpm run build" {
		t.Fatalf("expected pnpm run build, got %q", command)
	}
}

func TestResolveCommand_FallbackEmptyWithoutSignal(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType: "generic",
		Runtimes:    map[string]bool{},
	}
	response := ai.Response{
		Intent: "show me current directory",
	}

	command := ResolveCommand(response, environment)
	if command != "" {
		t.Fatalf("expected empty fallback command, got %q", command)
	}
}

func TestNormalizeCommand_GoBuildRootPackageToAllPackages(t *testing.T) {
	t.Parallel()

	environment := detector.Environment{
		ProjectType: "go",
	}

	normalized := NormalizeCommand("go build -v .", environment)
	if normalized != "go build -v ./..." {
		t.Fatalf("expected go build -v ./..., got %q", normalized)
	}
}
