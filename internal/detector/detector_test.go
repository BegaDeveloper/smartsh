package detector

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFindProjectRoot_FromNestedDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "repo")
	nestedDir := filepath.Join(projectRoot, "apps", "web")

	mustMkdirAll(t, nestedDir)
	mustWriteFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/repo\n")

	detectedRoot := findProjectRoot(nestedDir)
	if detectedRoot != projectRoot {
		t.Fatalf("expected root %q, got %q", projectRoot, detectedRoot)
	}
}

func TestDetectNestedMarkers_RespectsDepthAndSkipsIgnoredDirs(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "repo")
	mustMkdirAll(t, projectRoot)
	mustWriteFile(t, filepath.Join(projectRoot, "go.mod"), "module example.com/repo\n")

	// Included: depth <= 3 and valid marker.
	mustMkdirAll(t, filepath.Join(projectRoot, "services", "api"))
	mustWriteFile(t, filepath.Join(projectRoot, "services", "api", "package.json"), "{}\n")

	// Excluded: deeper than maxDepth.
	mustMkdirAll(t, filepath.Join(projectRoot, "very", "deep", "path", "service"))
	mustWriteFile(t, filepath.Join(projectRoot, "very", "deep", "path", "service", "pyproject.toml"), "[project]\n")

	// Excluded: ignored directory.
	mustMkdirAll(t, filepath.Join(projectRoot, "node_modules", "pkg"))
	mustWriteFile(t, filepath.Join(projectRoot, "node_modules", "pkg", "package.json"), "{}\n")

	markers := detectNestedMarkers(projectRoot, 3)
	markerSet := toSet(markers)

	if !markerSet["nested:services/api/package.json"] {
		t.Fatalf("expected nested marker for services/api/package.json, got %v", markers)
	}

	for marker := range markerSet {
		if strings.Contains(marker, "node_modules") {
			t.Fatalf("did not expect node_modules marker, got %q", marker)
		}
		if strings.Contains(marker, "very/deep/path/service/pyproject.toml") {
			t.Fatalf("did not expect deep marker beyond max depth, got %q", marker)
		}
	}
}

func TestDetectFiles_ContainsTopLevelAndNestedMarkers(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "repo")
	mustMkdirAll(t, filepath.Join(projectRoot, "apps", "client"))

	mustWriteFile(t, filepath.Join(projectRoot, "package.json"), "{}\n")
	mustWriteFile(t, filepath.Join(projectRoot, "nx.json"), "{}\n")
	mustWriteFile(t, filepath.Join(projectRoot, "apps", "client", "pyproject.toml"), "[project]\n")

	files := detectFiles(projectRoot)
	fileSet := toSet(files)

	if !fileSet["package.json"] {
		t.Fatalf("expected package.json in detected files, got %v", files)
	}
	if !fileSet["nx.json"] {
		t.Fatalf("expected nx.json in detected files, got %v", files)
	}
	if !fileSet["nested:apps/client/pyproject.toml"] {
		t.Fatalf("expected nested pyproject marker, got %v", files)
	}
}

func TestDetectLanguageHints_DetectsMultipleLanguages(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "repo")
	mustMkdirAll(t, filepath.Join(projectRoot, "src"))
	mustWriteFile(t, filepath.Join(projectRoot, "src", "main.go"), "package main\n")
	mustWriteFile(t, filepath.Join(projectRoot, "src", "app.ts"), "export {}\n")
	mustWriteFile(t, filepath.Join(projectRoot, "src", "program.py"), "print('ok')\n")

	hints := detectLanguageHints(projectRoot)
	hintSet := toSet(hints)

	if !hintSet["go"] {
		t.Fatalf("expected go hint, got %v", hints)
	}
	if !hintSet["javascript_typescript"] {
		t.Fatalf("expected javascript_typescript hint, got %v", hints)
	}
	if !hintSet["python"] {
		t.Fatalf("expected python hint, got %v", hints)
	}
}

func TestDetectDefaultShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		if shell := detectDefaultShell("windows"); shell != "cmd" {
			t.Fatalf("expected cmd on windows, got %q", shell)
		}
		return
	}

	t.Setenv("SHELL", "/bin/zsh")
	if shell := detectDefaultShell("darwin"); shell != "zsh" {
		t.Fatalf("expected zsh shell name, got %q", shell)
	}
}

func TestDetectWorkspaceKind_TableDriven(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		detectedFiles []string
		expected      string
	}{
		{
			name:          "nx workspace",
			detectedFiles: []string{"package.json", "nx.json"},
			expected:      "nx",
		},
		{
			name:          "angular workspace",
			detectedFiles: []string{"package.json", "angular.json"},
			expected:      "angular",
		},
		{
			name:          "javascript monorepo",
			detectedFiles: []string{"package.json", "pnpm-workspace.yaml"},
			expected:      "javascript_monorepo",
		},
		{
			name:          "single project default",
			detectedFiles: []string{"go.mod"},
			expected:      "single_project",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			workspaceKind := detectWorkspaceKind(testCase.detectedFiles)
			if workspaceKind != testCase.expected {
				t.Fatalf("expected workspace kind %q, got %q", testCase.expected, workspaceKind)
			}
		})
	}
}

func TestDetectPackageManager_TableDriven(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		detectedFiles []string
		runtimes      map[string]bool
		expected      string
	}{
		{
			name:          "pnpm preferred when lock and runtime exist",
			detectedFiles: []string{"package.json", "pnpm-lock.yaml", "yarn.lock"},
			runtimes:      map[string]bool{"pnpm": true, "yarn": true, "npm": true},
			expected:      "pnpm",
		},
		{
			name:          "yarn when lock and runtime exist",
			detectedFiles: []string{"package.json", "yarn.lock"},
			runtimes:      map[string]bool{"pnpm": false, "yarn": true, "npm": true},
			expected:      "yarn",
		},
		{
			name:          "bun lock file",
			detectedFiles: []string{"package.json", "bun.lockb"},
			runtimes:      map[string]bool{"npm": true},
			expected:      "bun",
		},
		{
			name:          "fallback npm",
			detectedFiles: []string{"package.json"},
			runtimes:      map[string]bool{"npm": true},
			expected:      "npm",
		},
		{
			name:          "no package manager",
			detectedFiles: []string{"go.mod"},
			runtimes:      map[string]bool{"npm": true},
			expected:      "",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			packageManager := detectPackageManager(testCase.detectedFiles, testCase.runtimes)
			if packageManager != testCase.expected {
				t.Fatalf("expected package manager %q, got %q", testCase.expected, packageManager)
			}
		})
	}
}

func TestDetectProjectType_TableDriven(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		detectedFiles []string
		expected      string
	}{
		{name: "node", detectedFiles: []string{"package.json"}, expected: "node"},
		{name: "rust", detectedFiles: []string{"Cargo.toml"}, expected: "rust"},
		{name: "go", detectedFiles: []string{"go.mod"}, expected: "go"},
		{name: "python", detectedFiles: []string{"requirements.txt"}, expected: "python"},
		{name: "dotnet", detectedFiles: []string{"*.csproj"}, expected: "dotnet"},
		{name: "java", detectedFiles: []string{"pom.xml"}, expected: "java"},
		{name: "c_cpp", detectedFiles: []string{"CMakeLists.txt"}, expected: "c_cpp"},
		{name: "docker", detectedFiles: []string{"compose.yaml"}, expected: "docker"},
		{name: "generic fallback", detectedFiles: []string{"README.md"}, expected: "generic"},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			projectType := detectProjectType(testCase.detectedFiles)
			if projectType != testCase.expected {
				t.Fatalf("expected project type %q, got %q", testCase.expected, projectType)
			}
		})
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if makeError := os.MkdirAll(path, 0o755); makeError != nil {
		t.Fatalf("mkdir %q: %v", path, makeError)
	}
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if writeError := os.WriteFile(path, []byte(content), 0o600); writeError != nil {
		t.Fatalf("write %q: %v", path, writeError)
	}
}

func toSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
