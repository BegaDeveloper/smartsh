package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type datasetRecord struct {
	Instruction string `json:"instruction"`
	Input       string `json:"input"`
	Output      string `json:"output"`
}

type rejectedLine struct {
	Line   int
	Reason string
	Raw    string
}

func main() {
	inputFile := flag.String("file", "./training/smartsh_train.jsonl", "path to JSONL dataset")
	inPlace := flag.Bool("in-place", true, "rewrite the dataset file in place (creates .bak)")
	outputFile := flag.String("out", "", "output file (only used when --in-place=false)")
	flag.Parse()

	if !*inPlace && strings.TrimSpace(*outputFile) == "" {
		fmt.Fprintln(os.Stderr, "--out is required when --in-place=false")
		os.Exit(2)
	}

	records, rejected, stats, fixError := fixDataset(*inputFile)
	if fixError != nil {
		fmt.Fprintln(os.Stderr, fixError)
		os.Exit(1)
	}

	var target string
	if *inPlace {
		target = *inputFile
	} else {
		target = *outputFile
	}

	if *inPlace {
		backupPath := target + ".bak"
		if renameError := os.Rename(target, backupPath); renameError != nil {
			fmt.Fprintf(os.Stderr, "backup failed: %v\n", renameError)
			os.Exit(1)
		}
	}

	if writeError := writeRecords(target, records); writeError != nil {
		fmt.Fprintln(os.Stderr, writeError)
		os.Exit(1)
	}

	rejectedFile := target + ".rejected.txt"
	if writeRejectedError := writeRejectedLines(rejectedFile, rejected); writeRejectedError != nil {
		fmt.Fprintln(os.Stderr, writeRejectedError)
		os.Exit(1)
	}

	fmt.Printf("fixed dataset written: %s\n", target)
	fmt.Printf("kept=%d fixed=%d dropped=%d\n", stats.kept, stats.fixed, stats.dropped)
	fmt.Printf("rejected lines report: %s\n", rejectedFile)
}

type fixStats struct {
	kept    int
	fixed   int
	dropped int
}

func fixDataset(path string) ([]datasetRecord, []rejectedLine, fixStats, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, nil, fixStats{}, fmt.Errorf("open file: %w", openError)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	records := make([]datasetRecord, 0, 1024)
	rejected := make([]rejectedLine, 0)
	stats := fixStats{}

	for scanner.Scan() {
		lineNumber++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		record, wasFixed, ok, reason := parseAndFixLine(raw)
		if !ok {
			stats.dropped++
			rejected = append(rejected, rejectedLine{Line: lineNumber, Reason: reason, Raw: raw})
			continue
		}

		if wasFixed {
			stats.fixed++
		} else {
			stats.kept++
		}

		records = append(records, record)
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, nil, fixStats{}, fmt.Errorf("scan file: %w", scanError)
	}

	return records, rejected, stats, nil
}

func parseAndFixLine(raw string) (datasetRecord, bool, bool, string) {
	// First try normal JSON.
	var record datasetRecord
	if json.Unmarshal([]byte(raw), &record) == nil {
		fixed, ok := normalizeRecord(record)
		if !ok {
			return datasetRecord{}, false, false, "invalid normalized record"
		}
		wasFixed := fixed != record
		return fixed, wasFixed, true, ""
	}

	// Salvage common “red line” case: valid instruction, but input/output were pasted as raw objects inside a quoted string.
	instruction, inputObject, outputObject, ok := salvageInstructionInputOutput(raw)
	if !ok {
		return datasetRecord{}, false, false, "could not salvage instruction/input/output"
	}

	inputString, ok := normalizeEnvironmentToString(inputObject)
	if !ok {
		return datasetRecord{}, false, false, "invalid input object"
	}
	outputString, ok := normalizeOutputToString(outputObject)
	if !ok {
		return datasetRecord{}, false, false, "invalid output object"
	}

	fixed := datasetRecord{
		Instruction: instruction,
		Input:       inputString,
		Output:      outputString,
	}
	final, ok := normalizeRecord(fixed)
	if !ok {
		return datasetRecord{}, false, false, "final normalization failed"
	}
	return final, true, true, ""
}

func normalizeRecord(record datasetRecord) (datasetRecord, bool) {
	instruction := strings.TrimSpace(record.Instruction)
	if instruction == "" {
		return datasetRecord{}, false
	}

	inputObject := map[string]any{}
	if json.Unmarshal([]byte(record.Input), &inputObject) != nil {
		return datasetRecord{}, false
	}

	outputObject := map[string]any{}
	if json.Unmarshal([]byte(record.Output), &outputObject) != nil {
		return datasetRecord{}, false
	}

	inputString, ok := normalizeEnvironmentToString(inputObject)
	if !ok {
		return datasetRecord{}, false
	}
	outputString, ok := normalizeOutputToString(outputObject)
	if !ok {
		return datasetRecord{}, false
	}

	return datasetRecord{
		Instruction: instruction,
		Input:       inputString,
		Output:      outputString,
	}, true
}

func normalizeEnvironmentToString(input map[string]any) (string, bool) {
	normalized := map[string]any{}
	for _, key := range []string{"os", "project_type", "workspace_kind", "package_manager", "runtimes", "detected_files"} {
		if value, ok := input[key]; ok {
			normalized[key] = value
		}
	}

	if normalized["os"] == nil || normalized["project_type"] == nil {
		return "", false
	}

	if normalized["workspace_kind"] == nil {
		normalized["workspace_kind"] = "single_project"
	}
	if normalized["runtimes"] == nil {
		normalized["runtimes"] = map[string]bool{}
	}
	if normalized["detected_files"] == nil {
		normalized["detected_files"] = []string{}
	}

	inputBytes, marshalError := json.Marshal(normalized)
	if marshalError != nil {
		return "", false
	}
	return string(inputBytes), true
}

func normalizeOutputToString(output map[string]any) (string, bool) {
	intent, _ := output["intent"].(string)
	command, _ := output["command"].(string)
	risk, _ := output["risk"].(string)

	confidenceValue, confidenceOk := output["confidence"]
	confidence, ok := asFloat64(confidenceValue)
	if !confidenceOk || !ok {
		return "", false
	}

	intent = strings.TrimSpace(intent)
	command = strings.TrimSpace(command)
	risk = strings.ToLower(strings.TrimSpace(risk))
	if intent == "" || command == "" {
		return "", false
	}
	if confidence < 0 || confidence > 1 {
		return "", false
	}
	if risk != "low" && risk != "medium" && risk != "high" {
		return "", false
	}

	// Fix known broken concatenation artifacts that sometimes sneak in from browser-generated datasets.
	command = strings.ReplaceAll(command, "\" + \"TODO\" + \"", "TODO")
	command = strings.ReplaceAll(command, "\" + \"", "")
	command = strings.ReplaceAll(command, "\\\"", "\"")

	normalized := map[string]any{
		"intent":     intent,
		"command":    command,
		"confidence": confidence,
		"risk":       risk,
	}
	outputBytes, marshalError := json.Marshal(normalized)
	if marshalError != nil {
		return "", false
	}
	return string(outputBytes), true
}

func asFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func salvageInstructionInputOutput(raw string) (string, map[string]any, map[string]any, bool) {
	instructionValue, ok := extractJSONStringField(raw, "instruction")
	if !ok {
		return "", nil, nil, false
	}

	inputObject, ok := extractJSONObjectField(raw, "input")
	if !ok {
		return "", nil, nil, false
	}
	outputObject, ok := extractJSONObjectField(raw, "output")
	if !ok {
		return "", nil, nil, false
	}

	return instructionValue, inputObject, outputObject, true
}

func extractJSONStringField(raw string, field string) (string, bool) {
	needle := `"` + field + `":`
	index := strings.Index(raw, needle)
	if index < 0 {
		return "", false
	}
	rest := strings.TrimSpace(raw[index+len(needle):])
	if !strings.HasPrefix(rest, "\"") {
		return "", false
	}

	// Find end of JSON string by scanning and honoring escapes.
	end := 1
	escaped := false
	for end < len(rest) {
		ch := rest[end]
		if escaped {
			escaped = false
			end++
			continue
		}
		if ch == '\\' {
			escaped = true
			end++
			continue
		}
		if ch == '"' {
			break
		}
		end++
	}
	if end >= len(rest) || rest[end] != '"' {
		return "", false
	}

	quoted := rest[:end+1]
	unquoted, unquoteError := strconv.Unquote(quoted)
	return unquoted, unquoteError == nil
}

func extractJSONObjectField(raw string, field string) (map[string]any, bool) {
	needle := `"` + field + `":`
	index := strings.Index(raw, needle)
	if index < 0 {
		return nil, false
	}
	rest := strings.TrimSpace(raw[index+len(needle):])
	if rest == "" {
		return nil, false
	}

	// Case 1: field is a quoted JSON string that itself contains a JSON object.
	if strings.HasPrefix(rest, "\"") {
		value, ok := extractJSONStringField(raw, field)
		if !ok {
			return nil, false
		}
		obj := map[string]any{}
		if json.Unmarshal([]byte(value), &obj) != nil {
			return nil, false
		}
		return obj, true
	}

	// Case 2: field is a raw JSON object (starts with '{').
	if !strings.HasPrefix(rest, "{") {
		return nil, false
	}

	objectString, ok := readBalancedObject(rest)
	if !ok {
		return nil, false
	}

	obj := map[string]any{}
	if json.Unmarshal([]byte(objectString), &obj) != nil {
		return nil, false
	}
	return obj, true
}

func readBalancedObject(s string) (string, bool) {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

func writeRecords(path string, records []datasetRecord) error {
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		return fmt.Errorf("mkdir: %w", makeError)
	}

	file, createError := os.Create(path)
	if createError != nil {
		return fmt.Errorf("create file: %w", createError)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, record := range records {
		lineBytes, marshalError := json.Marshal(record)
		if marshalError != nil {
			return fmt.Errorf("marshal record: %w", marshalError)
		}
		if _, writeError := writer.Write(append(lineBytes, '\n')); writeError != nil {
			return fmt.Errorf("write record: %w", writeError)
		}
	}
	if flushError := writer.Flush(); flushError != nil {
		return fmt.Errorf("flush: %w", flushError)
	}
	return nil
}

func writeRejectedLines(path string, rejected []rejectedLine) error {
	file, createError := os.Create(path)
	if createError != nil {
		return fmt.Errorf("create rejected file: %w", createError)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, entry := range rejected {
		line := fmt.Sprintf("line=%d reason=%s raw=%s\n", entry.Line, entry.Reason, entry.Raw)
		if _, writeError := writer.WriteString(line); writeError != nil {
			return fmt.Errorf("write rejected line: %w", writeError)
		}
	}
	if flushError := writer.Flush(); flushError != nil {
		return fmt.Errorf("flush rejected file: %w", flushError)
	}
	return nil
}
