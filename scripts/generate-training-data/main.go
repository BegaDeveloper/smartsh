package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type datasetRecord struct {
	Instruction string `json:"instruction"`
	Input       string `json:"input"`
	Output      string `json:"output"`
}

func main() {
	targetFile := flag.String("file", "./training/smartsh_train.jsonl", "path to JSONL dataset")
	count := flag.Int("count", 300, "number of records to append")
	seed := flag.Int64("seed", time.Now().Unix(), "random seed")
	flag.Parse()

	if *count <= 0 {
		fmt.Fprintln(os.Stderr, "--count must be > 0")
		os.Exit(2)
	}

	records := generateRecords(*count, *seed)
	if writeError := appendRecords(*targetFile, records); writeError != nil {
		fmt.Fprintln(os.Stderr, writeError)
		os.Exit(1)
	}

	fmt.Printf("appended %d records to %s (seed=%d)\n", len(records), *targetFile, *seed)
}

func generateRecords(count int, seed int64) []datasetRecord {
	random := rand.New(rand.NewSource(seed))

	commitMessages := []string{
		"fix: login bug",
		"fix: null pointer crash",
		"chore: update deps",
		"refactor: simplify handler",
		"feat: add health endpoint",
		"docs: update readme",
		"test: add coverage",
	}
	branchNames := []string{"feature/auth", "feature/payments", "hotfix/ci", "chore/deps", "bugfix/timeout", "feature/ui"}
	databases := []string{"app", "app_test", "smartsh", "example"}
	dockerServices := []string{"api", "web", "db", "redis", "worker"}
	stashMessages := []string{"wip", "wip: debugging", "temp", "before refactor"}
	remoteNames := []string{"origin", "upstream"}
	scriptNames := []string{"./scripts/build.sh", "./scripts/test.sh", "./deploy.sh"}

	oses := []string{"darwin", "linux", "windows"}
	packageManagers := []string{"npm", "pnpm", "yarn"}
	workspaceKinds := []string{"single_project", "nx", "angular", "javascript_monorepo"}

	makeRecord := func(instruction string, env map[string]any, out map[string]any) datasetRecord {
		inputBytes, _ := json.Marshal(env)
		outputBytes, _ := json.Marshal(out)
		return datasetRecord{
			Instruction: instruction,
			Input:       string(inputBytes),
			Output:      string(outputBytes),
		}
	}

	records := make([]datasetRecord, 0, count)
	for i := 0; i < count; i++ {
		osValue := oses[i%len(oses)]
		workspaceKind := workspaceKinds[(i/3)%len(workspaceKinds)]

		switch i % 24 {
		case 0:
			// Git: add + commit + push
			message := commitMessages[random.Intn(len(commitMessages))]
			remote := remoteNames[random.Intn(len(remoteNames))]
			targetBranch := "main"
			instruction := "commit and push my changes"
			command := fmt.Sprintf("git add . && git commit -m %q && git push %s %s", message, remote, targetBranch)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "sync", "command": command, "confidence": 0.9, "risk": "medium"},
			))
		case 1:
			// Git: create branch
			branch := branchNames[random.Intn(len(branchNames))]
			instruction := "create a new git branch for my work"
			command := fmt.Sprintf("git checkout -b %s", branch)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "change", "command": command, "confidence": 0.92, "risk": "low"},
			))
		case 2:
			// Git: reset hard (high risk)
			instruction := "discard all local changes and reset hard"
			command := "git reset --hard"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "cleanup", "command": command, "confidence": 0.85, "risk": "high"},
			))
		case 3:
			// Docker: start compose
			instruction := "start docker compose services"
			command := "docker compose up -d"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{"docker-compose.yml"},
				},
				map[string]any{"intent": "run", "command": command, "confidence": 0.92, "risk": "medium"},
			))
		case 4:
			// Docker: rebuild + start
			instruction := "rebuild docker compose and start"
			command := "docker compose up -d --build"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{"compose.yaml"},
				},
				map[string]any{"intent": "run", "command": command, "confidence": 0.9, "risk": "medium"},
			))
		case 5:
			// Database: drop and recreate (high risk)
			db := databases[random.Intn(len(databases))]
			instruction := "clear the database (drop and recreate)"
			command := fmt.Sprintf("docker compose exec db psql -U postgres -c 'DROP DATABASE IF EXISTS %s; CREATE DATABASE %s;'", db, db)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{"docker-compose.yml"},
				},
				map[string]any{"intent": "cleanup", "command": command, "confidence": 0.78, "risk": "high"},
			))
		case 6:
			// Node: install (pm-aware)
			pm := packageManagers[random.Intn(len(packageManagers))]
			instruction := "install project dependencies"
			command := pm + " install"
			if pm == "npm" {
				command = "npm ci"
			}
			detected := []string{"package.json"}
			if pm == "pnpm" {
				detected = append(detected, "pnpm-lock.yaml")
			}
			if pm == "yarn" {
				detected = append(detected, "yarn.lock")
			}
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":              osValue,
					"project_type":    "node",
					"workspace_kind":  workspaceKind,
					"package_manager": pm,
					"runtimes":        map[string]bool{"node": true, pm: true},
					"detected_files":  detected,
				},
				map[string]any{"intent": "install", "command": command, "confidence": 0.93, "risk": "low"},
			))
		case 7:
			// Node: run dev
			pm := packageManagers[random.Intn(len(packageManagers))]
			instruction := "run dev server"
			command := pm + " run dev"
			if pm == "yarn" {
				command = "yarn dev"
			}
			if pm == "pnpm" {
				command = "pnpm run dev"
			}
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":              osValue,
					"project_type":    "node",
					"workspace_kind":  workspaceKind,
					"package_manager": pm,
					"runtimes":        map[string]bool{"node": true, pm: true},
					"detected_files":  []string{"package.json"},
				},
				map[string]any{"intent": "run", "command": command, "confidence": 0.9, "risk": "low"},
			))
		case 8:
			// Go: build all
			instruction := "build all go packages"
			command := "go build ./..."
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "go",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"go": true},
					"detected_files": []string{"go.mod"},
				},
				map[string]any{"intent": "build", "command": command, "confidence": 0.98, "risk": "low"},
			))
		case 9:
			// Go: test
			instruction := "run go tests"
			command := "go test ./..."
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "go",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"go": true},
					"detected_files": []string{"go.mod"},
				},
				map[string]any{"intent": "test", "command": command, "confidence": 0.96, "risk": "low"},
			))
		case 10:
			// System: kill port (high risk)
			port := 3000 + (i % 50)
			instruction := fmt.Sprintf("kill the process on port %d", port)
			command := fmt.Sprintf("lsof -ti:%d | xargs kill -9", port)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             "darwin",
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{},
					"detected_files": []string{},
				},
				map[string]any{"intent": "system", "command": command, "confidence": 0.78, "risk": "high"},
			))
		case 11:
			// Docker: prune (high risk)
			instruction := "remove unused docker resources"
			command := "docker system prune -a --volumes"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{},
				},
				map[string]any{"intent": "cleanup", "command": command, "confidence": 0.8, "risk": "high"},
			))
		case 12:
			// Git: status (short, common)
			instruction := "what changed?"
			command := "git status -sb"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "inspect", "command": command, "confidence": 0.96, "risk": "low"},
			))
		case 13:
			// Git: stash (short)
			message := stashMessages[random.Intn(len(stashMessages))]
			instruction := "stash my work"
			command := fmt.Sprintf("git stash push -m %q", message)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "change", "command": command, "confidence": 0.9, "risk": "low"},
			))
		case 14:
			// Git: stash pop (medium risk)
			instruction := "apply my last stash"
			command := "git stash pop"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "change", "command": command, "confidence": 0.86, "risk": "medium"},
			))
		case 15:
			// Git: fetch + rebase pull (slightly longer)
			remote := remoteNames[random.Intn(len(remoteNames))]
			instruction := "pull latest changes safely"
			command := fmt.Sprintf("git fetch %s && git pull --rebase", remote)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"git": true},
					"detected_files": []string{".git"},
				},
				map[string]any{"intent": "sync", "command": command, "confidence": 0.82, "risk": "low"},
			))
		case 16:
			// Docker: logs (short)
			service := dockerServices[random.Intn(len(dockerServices))]
			instruction := "tail docker logs"
			command := fmt.Sprintf("docker compose logs -f %s", service)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{"docker-compose.yml"},
				},
				map[string]any{"intent": "inspect", "command": command, "confidence": 0.92, "risk": "low"},
			))
		case 17:
			// Docker: exec into container (medium risk)
			service := dockerServices[random.Intn(len(dockerServices))]
			instruction := "shell into the container"
			command := fmt.Sprintf("docker compose exec %s sh", service)
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "docker",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"docker": true},
					"detected_files": []string{"docker-compose.yml"},
				},
				map[string]any{"intent": "inspect", "command": command, "confidence": 0.78, "risk": "medium"},
			))
		case 18:
			// Node: tests (medium length)
			pm := packageManagers[random.Intn(len(packageManagers))]
			instruction := "run tests"
			command := pm + " test"
			if pm == "pnpm" {
				command = "pnpm test"
			}
			if pm == "yarn" {
				command = "yarn test"
			}
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":              osValue,
					"project_type":    "node",
					"workspace_kind":  workspaceKind,
					"package_manager": pm,
					"runtimes":        map[string]bool{"node": true, pm: true},
					"detected_files":  []string{"package.json"},
				},
				map[string]any{"intent": "test", "command": command, "confidence": 0.9, "risk": "low"},
			))
		case 19:
			// Node: build (short)
			pm := packageManagers[random.Intn(len(packageManagers))]
			instruction := "build the app"
			command := pm + " run build"
			if pm == "yarn" {
				command = "yarn build"
			}
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":              osValue,
					"project_type":    "node",
					"workspace_kind":  workspaceKind,
					"package_manager": pm,
					"runtimes":        map[string]bool{"node": true, pm: true},
					"detected_files":  []string{"package.json"},
				},
				map[string]any{"intent": "build", "command": command, "confidence": 0.9, "risk": "low"},
			))
		case 20:
			// Python: run tests (short)
			instruction := "run python tests"
			command := "python3 -m pytest"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "python",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"python": true},
					"detected_files": []string{"pyproject.toml"},
				},
				map[string]any{"intent": "test", "command": command, "confidence": 0.88, "risk": "low"},
			))
		case 21:
			// .NET: restore + test (medium length)
			instruction := "restore and test dotnet project"
			command := "dotnet restore && dotnet test"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             "windows",
					"project_type":   "dotnet",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"dotnet": true},
					"detected_files": []string{"*.sln"},
				},
				map[string]any{"intent": "test", "command": command, "confidence": 0.84, "risk": "low"},
			))
		case 22:
			// Java: clean + test (medium length)
			instruction := "run maven tests"
			command := "mvn clean test"
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "java",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{"mvn": true},
					"detected_files": []string{"pom.xml"},
				},
				map[string]any{"intent": "test", "command": command, "confidence": 0.9, "risk": "low"},
			))
		case 23:
			// Generic: run a local script (medium risk)
			script := scriptNames[random.Intn(len(scriptNames))]
			instruction := "run the project script"
			command := script
			records = append(records, makeRecord(instruction,
				map[string]any{
					"os":             osValue,
					"project_type":   "generic",
					"workspace_kind": "single_project",
					"runtimes":       map[string]bool{},
					"detected_files": []string{strings.TrimPrefix(script, "./")},
				},
				map[string]any{"intent": "run", "command": command, "confidence": 0.78, "risk": "medium"},
			))
		}
	}

	return records
}

func appendRecords(path string, records []datasetRecord) error {
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		return fmt.Errorf("mkdir: %w", makeError)
	}

	file, openError := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if openError != nil {
		return fmt.Errorf("open: %w", openError)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, record := range records {
		lineBytes, marshalError := json.Marshal(record)
		if marshalError != nil {
			return fmt.Errorf("marshal: %w", marshalError)
		}
		line := string(lineBytes)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, writeError := writer.WriteString(line + "\n"); writeError != nil {
			return fmt.Errorf("write: %w", writeError)
		}
	}
	return writer.Flush()
}
