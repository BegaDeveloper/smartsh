package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type datasetRecord struct {
	Instruction string `json:"instruction"`
	Input       string `json:"input"`
	Output      string `json:"output"`
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

	scoreError := scoreDataset(*dataFile)
	if scoreError != nil {
		fmt.Fprintln(os.Stderr, scoreError)
		os.Exit(1)
	}
}

func scoreDataset(path string) error {
	file, openError := os.Open(path)
	if openError != nil {
		return fmt.Errorf("open file: %w", openError)
	}
	defer file.Close()

	total := 0
	invalid := 0
	commandCounts := map[string]int{}
	instructionCounts := map[string]int{}
	intentCounts := map[string]int{}
	riskCounts := map[string]int{}
	suspiciousHits := map[string]int{
		"rm -rf":              0,
		"git reset --hard":    0,
		"docker system prune": 0,
		"pipe-to-shell":       0,
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		total++

		record := datasetRecord{}
		if unmarshalError := json.Unmarshal([]byte(line), &record); unmarshalError != nil {
			invalid++
			continue
		}

		output := expectedOutput{}
		if unmarshalError := json.Unmarshal([]byte(record.Output), &output); unmarshalError != nil {
			invalid++
			continue
		}

		intent := strings.ToLower(strings.TrimSpace(output.Intent))
		risk := strings.ToLower(strings.TrimSpace(output.Risk))
		command := strings.TrimSpace(output.Command)
		instruction := strings.ToLower(strings.TrimSpace(record.Instruction))

		intentCounts[intent]++
		riskCounts[risk]++
		commandCounts[command]++
		instructionCounts[instruction]++

		commandLower := strings.ToLower(command)
		if strings.Contains(commandLower, "rm -rf") {
			suspiciousHits["rm -rf"]++
		}
		if strings.Contains(commandLower, "git reset --hard") {
			suspiciousHits["git reset --hard"]++
		}
		if strings.Contains(commandLower, "docker system prune") {
			suspiciousHits["docker system prune"]++
		}
		if strings.Contains(commandLower, "| sh") || strings.Contains(commandLower, "| bash") {
			suspiciousHits["pipe-to-shell"]++
		}
	}
	if scanError := scanner.Err(); scanError != nil {
		return fmt.Errorf("scan file: %w", scanError)
	}

	duplicateCount := 0
	for _, count := range commandCounts {
		if count > 1 {
			duplicateCount += count - 1
		}
	}
	duplicateInstructionCount := 0
	for _, count := range instructionCounts {
		if count > 1 {
			duplicateInstructionCount += count - 1
		}
	}

	fmt.Printf("dataset: %s\n", path)
	fmt.Printf("records_total=%d invalid=%d\n", total, invalid)
	if total > 0 {
		fmt.Printf("duplicate_commands=%d (%.2f%%)\n", duplicateCount, float64(duplicateCount)*100/float64(total))
		fmt.Printf("duplicate_instructions=%d (%.2f%%)\n", duplicateInstructionCount, float64(duplicateInstructionCount)*100/float64(total))
	}

	fmt.Println("risk_distribution:")
	printSortedCounts(riskCounts)
	fmt.Println("intent_distribution:")
	printSortedCounts(intentCounts)
	fmt.Println("suspicious_command_hits:")
	printSortedCounts(suspiciousHits)

	return nil
}

func printSortedCounts(counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Printf("  %s: %d\n", key, counts[key])
	}
}
