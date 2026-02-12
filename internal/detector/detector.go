package detector

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Environment struct {
	OS             string              `json:"os"`
	WorkingDir     string              `json:"working_dir"`
	ProjectRoot    string              `json:"project_root"`
	ProjectType    string              `json:"project_type"`
	WorkspaceKind  string              `json:"workspace_kind"`
	PackageManager string              `json:"package_manager,omitempty"`
	NodeScripts    map[string]string   `json:"node_scripts,omitempty"`
	NxTargets      map[string][]string `json:"nx_targets,omitempty"`
	LanguageHints  []string            `json:"language_hints,omitempty"`
	DetectedFiles  []string            `json:"detected_files"`
	Runtimes       map[string]bool     `json:"runtimes"`
	Metadata       map[string]string   `json:"metadata,omitempty"`
}

func DetectEnvironment() (Environment, error) {
	workingDir, workingDirError := os.Getwd()
	if workingDirError != nil {
		return Environment{}, workingDirError
	}

	projectRoot := findProjectRoot(workingDir)
	detectedFiles := detectFiles(projectRoot)
	projectType := detectProjectType(detectedFiles)
	runtimes := detectRuntimes()
	workspaceKind := detectWorkspaceKind(detectedFiles)
	packageManager := detectPackageManager(detectedFiles, runtimes)
	nodeScripts := detectNodeScripts(projectRoot)
	nxTargets := detectNxTargets(projectRoot, detectedFiles)
	languageHints := detectLanguageHints(projectRoot)
	relativeWorkingDir, relativeError := filepath.Rel(projectRoot, workingDir)
	if relativeError != nil {
		relativeWorkingDir = "."
	}

	return Environment{
		OS:             runtime.GOOS,
		WorkingDir:     workingDir,
		ProjectRoot:    projectRoot,
		ProjectType:    projectType,
		WorkspaceKind:  workspaceKind,
		PackageManager: packageManager,
		NodeScripts:    nodeScripts,
		NxTargets:      nxTargets,
		LanguageHints:  languageHints,
		DetectedFiles:  detectedFiles,
		Runtimes:       runtimes,
		Metadata: map[string]string{
			"shell":                detectDefaultShell(runtime.GOOS),
			"relative_working_dir": relativeWorkingDir,
		},
	}, nil
}

func detectFiles(projectRoot string) []string {
	candidates := []string{
		"package.json",
		"pnpm-lock.yaml",
		"pnpm-workspace.yaml",
		"yarn.lock",
		"bun.lockb",
		"bun.lock",
		"nx.json",
		"angular.json",
		"turbo.json",
		"lerna.json",
		"go.mod",
		"go.work",
		"pyproject.toml",
		"requirements.txt",
		"Pipfile",
		"poetry.lock",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"settings.gradle",
		"settings.gradle.kts",
		"Cargo.toml",
		"CMakeLists.txt",
		"Makefile",
		"compose.yaml",
		"docker-compose.yml",
		"docker-compose.yaml",
		"Dockerfile",
	}

	foundSet := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		path := filepath.Join(projectRoot, candidate)
		if _, statError := os.Stat(path); statError == nil {
			foundSet[candidate] = true
		}
	}

	csprojMatches, _ := filepath.Glob(filepath.Join(projectRoot, "*.csproj"))
	if len(csprojMatches) > 0 {
		foundSet["*.csproj"] = true
	}
	slnMatches, _ := filepath.Glob(filepath.Join(projectRoot, "*.sln"))
	if len(slnMatches) > 0 {
		foundSet["*.sln"] = true
	}

	for _, nestedMarker := range detectNestedMarkers(projectRoot, 3) {
		foundSet[nestedMarker] = true
	}

	found := make([]string, 0, len(foundSet))
	for marker := range foundSet {
		found = append(found, marker)
	}
	sort.Strings(found)
	return found
}

func detectProjectType(detectedFiles []string) string {
	fileSet := make(map[string]bool, len(detectedFiles))
	for _, name := range detectedFiles {
		fileSet[name] = true
	}

	switch {
	case fileSet["package.json"]:
		return "node"
	case fileSet["Cargo.toml"]:
		return "rust"
	case fileSet["go.mod"]:
		return "go"
	case fileSet["pyproject.toml"] || fileSet["requirements.txt"] || fileSet["Pipfile"]:
		return "python"
	case fileSet["*.csproj"] || fileSet["*.sln"]:
		return "dotnet"
	case fileSet["pom.xml"] || fileSet["build.gradle"] || fileSet["build.gradle.kts"]:
		return "java"
	case fileSet["CMakeLists.txt"] || fileSet["Makefile"]:
		return "c_cpp"
	case fileSet["compose.yaml"] || fileSet["docker-compose.yml"] || fileSet["docker-compose.yaml"] || fileSet["Dockerfile"]:
		return "docker"
	default:
		return "generic"
	}
}

func detectRuntimes() map[string]bool {
	commands := map[string][]string{
		"node":   {"node"},
		"npm":    {"npm"},
		"pnpm":   {"pnpm"},
		"yarn":   {"yarn"},
		"dotnet": {"dotnet"},
		"python": {"python3", "python"},
		"java":   {"java"},
		"go":     {"go"},
		"gcc":    {"gcc"},
		"clang":  {"clang"},
		"docker": {"docker"},
		"mvn":    {"mvn"},
		"gradle": {"gradle"},
		"make":   {"make"},
	}

	availability := make(map[string]bool, len(commands))
	for runtimeName, binaryNames := range commands {
		availability[runtimeName] = hasAnyBinary(binaryNames)
	}
	return availability
}

func hasAnyBinary(binaryNames []string) bool {
	for _, binaryName := range binaryNames {
		if _, lookupError := exec.LookPath(binaryName); lookupError == nil {
			return true
		}
	}
	return false
}

func detectDefaultShell(goos string) string {
	if goos == "windows" {
		return "cmd"
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		return "sh"
	}
	return filepath.Base(shell)
}

func findProjectRoot(startDir string) string {
	rootMarkers := []string{
		"go.mod",
		"go.work",
		"package.json",
		"pyproject.toml",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"nx.json",
		"angular.json",
		"pnpm-workspace.yaml",
	}

	currentDir := startDir
	for {
		if hasAnyMarker(currentDir, rootMarkers) || hasGlobMatches(currentDir, "*.sln") || hasGlobMatches(currentDir, "*.csproj") {
			return currentDir
		}

		parentDir := filepath.Dir(currentDir)
		if parentDir == currentDir {
			return startDir
		}
		currentDir = parentDir
	}
}

func hasAnyMarker(dir string, markers []string) bool {
	for _, marker := range markers {
		if _, statError := os.Stat(filepath.Join(dir, marker)); statError == nil {
			return true
		}
	}
	return false
}

func hasGlobMatches(dir string, pattern string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	return len(matches) > 0
}

func detectNestedMarkers(projectRoot string, maxDepth int) []string {
	markers := map[string]bool{}
	interestingNames := map[string]bool{
		"package.json":        true,
		"go.mod":              true,
		"pyproject.toml":      true,
		"requirements.txt":    true,
		"pom.xml":             true,
		"build.gradle":        true,
		"build.gradle.kts":    true,
		"Cargo.toml":          true,
		"CMakeLists.txt":      true,
		"Dockerfile":          true,
		"docker-compose.yml":  true,
		"docker-compose.yaml": true,
		"compose.yaml":        true,
	}

	_ = filepath.WalkDir(projectRoot, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return nil
		}

		relativePath, relError := filepath.Rel(projectRoot, path)
		if relError != nil || relativePath == "." {
			return nil
		}

		depth := strings.Count(relativePath, string(os.PathSeparator))
		if entry.IsDir() {
			if depth >= maxDepth {
				return filepath.SkipDir
			}
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		if depth > maxDepth {
			return nil
		}

		fileName := entry.Name()
		if interestingNames[fileName] || strings.HasSuffix(fileName, ".csproj") || strings.HasSuffix(fileName, ".sln") {
			markers["nested:"+relativePath] = true
		}

		return nil
	})

	results := make([]string, 0, len(markers))
	for marker := range markers {
		results = append(results, marker)
	}
	sort.Strings(results)
	return results
}

func detectWorkspaceKind(detectedFiles []string) string {
	fileSet := make(map[string]bool, len(detectedFiles))
	for _, fileName := range detectedFiles {
		fileSet[fileName] = true
	}

	switch {
	case fileSet["nx.json"]:
		return "nx"
	case fileSet["angular.json"]:
		return "angular"
	case fileSet["pnpm-workspace.yaml"] || fileSet["turbo.json"] || fileSet["lerna.json"]:
		return "javascript_monorepo"
	default:
		return "single_project"
	}
}

func detectPackageManager(detectedFiles []string, runtimes map[string]bool) string {
	fileSet := make(map[string]bool, len(detectedFiles))
	for _, fileName := range detectedFiles {
		fileSet[fileName] = true
	}

	switch {
	case fileSet["pnpm-lock.yaml"] && runtimes["pnpm"]:
		return "pnpm"
	case fileSet["yarn.lock"] && runtimes["yarn"]:
		return "yarn"
	case fileSet["bun.lockb"] || fileSet["bun.lock"]:
		return "bun"
	case fileSet["package.json"] && runtimes["npm"]:
		return "npm"
	default:
		return ""
	}
}

func detectLanguageHints(projectRoot string) []string {
	hints := map[string]bool{}
	_ = filepath.WalkDir(projectRoot, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return nil
		}

		relativePath, relError := filepath.Rel(projectRoot, path)
		if relError != nil || relativePath == "." {
			return nil
		}

		depth := strings.Count(relativePath, string(os.PathSeparator))
		if entry.IsDir() {
			if depth >= 2 {
				return filepath.SkipDir
			}
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		extension := strings.ToLower(filepath.Ext(entry.Name()))
		switch extension {
		case ".go":
			hints["go"] = true
		case ".ts", ".tsx", ".js", ".mjs", ".cjs":
			hints["javascript_typescript"] = true
		case ".py":
			hints["python"] = true
		case ".java":
			hints["java"] = true
		case ".cs":
			hints["dotnet"] = true
		case ".c", ".cc", ".cpp", ".h", ".hpp":
			hints["c_cpp"] = true
		case ".rs":
			hints["rust"] = true
		}
		return nil
	})

	result := make([]string, 0, len(hints))
	for hint := range hints {
		result = append(result, hint)
	}
	sort.Strings(result)
	return result
}

func detectNodeScripts(projectRoot string) map[string]string {
	packageJSONPath := filepath.Join(projectRoot, "package.json")
	content, readError := os.ReadFile(packageJSONPath)
	if readError != nil {
		return map[string]string{}
	}

	payload := struct {
		Scripts map[string]string `json:"scripts"`
	}{}
	if unmarshalError := json.Unmarshal(content, &payload); unmarshalError != nil {
		return map[string]string{}
	}
	if payload.Scripts == nil {
		return map[string]string{}
	}

	return payload.Scripts
}

func detectNxTargets(projectRoot string, detectedFiles []string) map[string][]string {
	fileSet := make(map[string]bool, len(detectedFiles))
	for _, detectedFile := range detectedFiles {
		fileSet[detectedFile] = true
	}
	if !fileSet["nx.json"] {
		return map[string][]string{}
	}

	targetMap := map[string][]string{}
	_ = filepath.WalkDir(projectRoot, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return nil
		}

		relativePath, relError := filepath.Rel(projectRoot, path)
		if relError != nil {
			return nil
		}

		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" || entry.Name() == "dist" || entry.Name() == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		fileName := entry.Name()
		if fileName != "project.json" && fileName != "package.json" {
			return nil
		}

		content, readError := os.ReadFile(path)
		if readError != nil {
			return nil
		}

		switch fileName {
		case "project.json":
			projectPayload := struct {
				Name    string                     `json:"name"`
				Targets map[string]json.RawMessage `json:"targets"`
			}{}
			if unmarshalError := json.Unmarshal(content, &projectPayload); unmarshalError != nil {
				return nil
			}

			projectName := strings.TrimSpace(projectPayload.Name)
			if projectName == "" {
				projectName = inferProjectNameFromPath(relativePath)
			}
			addNxTargets(targetMap, projectName, projectPayload.Targets)

		case "package.json":
			packagePayload := struct {
				Name string `json:"name"`
				Nx   struct {
					Targets map[string]json.RawMessage `json:"targets"`
				} `json:"nx"`
			}{}
			if unmarshalError := json.Unmarshal(content, &packagePayload); unmarshalError != nil {
				return nil
			}
			projectName := strings.TrimSpace(packagePayload.Name)
			if projectName == "" {
				projectName = inferProjectNameFromPath(relativePath)
			}
			addNxTargets(targetMap, projectName, packagePayload.Nx.Targets)
		}

		return nil
	})

	return targetMap
}

func addNxTargets(targetMap map[string][]string, projectName string, targets map[string]json.RawMessage) {
	if strings.TrimSpace(projectName) == "" || len(targets) == 0 {
		return
	}
	existing := map[string]bool{}
	for _, targetName := range targetMap[projectName] {
		existing[targetName] = true
	}
	for targetName := range targets {
		if !existing[targetName] {
			targetMap[projectName] = append(targetMap[projectName], targetName)
		}
	}
	sort.Strings(targetMap[projectName])
}

func inferProjectNameFromPath(relativePath string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(relativePath))
	normalized = strings.TrimSuffix(normalized, "/project.json")
	normalized = strings.TrimSuffix(normalized, "/package.json")
	normalized = strings.TrimPrefix(normalized, "apps/")
	normalized = strings.TrimPrefix(normalized, "libs/")
	normalized = strings.TrimPrefix(normalized, "packages/")
	return strings.ReplaceAll(normalized, "/", "-")
}
