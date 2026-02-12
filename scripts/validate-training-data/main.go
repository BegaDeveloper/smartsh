package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type datasetRecord struct {
	Instruction string `json:"instruction"`
	Input       string `json:"input"`
	Output      string `json:"output"`
}

type environmentInput struct {
	OS             string          `json:"os"`
	ProjectType    string          `json:"project_type"`
	WorkspaceKind  string          `json:"workspace_kind"`
	PackageManager string          `json:"package_manager,omitempty"`
	Runtimes       map[string]bool `json:"runtimes"`
	DetectedFiles  []string        `json:"detected_files"`
}

type expectedOutput struct {
	Intent     string  `json:"intent"`
	Command    string  `json:"command"`
	Confidence float64 `json:"confidence"`
	Risk       string  `json:"risk"`
}

func main() {
	dataFile := flag.String("file", "./training/smartsh_train.jsonl", "path to JSONL dataset")
	flag.Parse()

	validationErrors := validateDatasetFile(*dataFile)
	if len(validationErrors) > 0 {
		for _, validationError := range validationErrors {
			fmt.Fprintln(os.Stderr, validationError)
		}
		os.Exit(1)
	}

	fmt.Printf("dataset validation passed: %s\n", *dataFile)
}

func validateDatasetFile(path string) []string {
	file, openError := os.Open(path)
	if openError != nil {
		return []string{fmt.Sprintf("open file error: %v", openError)}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	validationErrors := make([]string, 0)
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		lineErrors := validateLine(lineNumber, line)
		validationErrors = append(validationErrors, lineErrors...)
	}
	if scanError := scanner.Err(); scanError != nil {
		validationErrors = append(validationErrors, fmt.Sprintf("scan file error: %v", scanError))
	}
	return validationErrors
}

func validateLine(lineNumber int, line string) []string {
	record := datasetRecord{}
	if parseError := json.Unmarshal([]byte(line), &record); parseError != nil {
		return []string{fmt.Sprintf("line %d: invalid JSON record: %v", lineNumber, parseError)}
	}

	errors := make([]string, 0)
	if strings.TrimSpace(record.Instruction) == "" {
		errors = append(errors, fmt.Sprintf("line %d: instruction is required", lineNumber))
	}
	if strings.TrimSpace(record.Input) == "" {
		errors = append(errors, fmt.Sprintf("line %d: input is required", lineNumber))
	}
	if strings.TrimSpace(record.Output) == "" {
		errors = append(errors, fmt.Sprintf("line %d: output is required", lineNumber))
	}
	if len(errors) > 0 {
		return errors
	}

	environment := environmentInput{}
	inputDecoder := json.NewDecoder(strings.NewReader(record.Input))
	inputDecoder.DisallowUnknownFields()
	if parseError := inputDecoder.Decode(&environment); parseError != nil {
		errors = append(errors, fmt.Sprintf("line %d: input is not valid strict JSON string object: %v", lineNumber, parseError))
	} else {
		if strings.TrimSpace(environment.OS) == "" {
			errors = append(errors, fmt.Sprintf("line %d: input.os is required", lineNumber))
		}
		if strings.TrimSpace(environment.ProjectType) == "" {
			errors = append(errors, fmt.Sprintf("line %d: input.project_type is required", lineNumber))
		}
	}

	output := expectedOutput{}
	outputDecoder := json.NewDecoder(strings.NewReader(record.Output))
	outputDecoder.DisallowUnknownFields()
	if parseError := outputDecoder.Decode(&output); parseError != nil {
		errors = append(errors, fmt.Sprintf("line %d: output is not valid strict JSON string object: %v", lineNumber, parseError))
		return errors
	}

	if strings.TrimSpace(output.Intent) == "" {
		errors = append(errors, fmt.Sprintf("line %d: output.intent is required", lineNumber))
	}
	if strings.TrimSpace(output.Command) == "" {
		errors = append(errors, fmt.Sprintf("line %d: output.command is required", lineNumber))
	}
	if output.Confidence < 0 || output.Confidence > 1 {
		errors = append(errors, fmt.Sprintf("line %d: output.confidence must be in [0,1]", lineNumber))
	}
	switch strings.ToLower(strings.TrimSpace(output.Risk)) {
	case "low", "medium", "high":
	default:
		errors = append(errors, fmt.Sprintf("line %d: output.risk must be low|medium|high", lineNumber))
	}

	return errors
}
