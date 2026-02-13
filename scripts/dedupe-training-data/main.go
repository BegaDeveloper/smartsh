package main

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
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

func main() {
	inputFile := flag.String("file", "./training/smartsh_train.jsonl", "path to JSONL dataset")
	outputFile := flag.String("out", "./training/smartsh_train.deduped.jsonl", "path to write deduped JSONL dataset")
	flag.Parse()

	records, duplicates, err := dedupe(*inputFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if writeError := writeDataset(*outputFile, records); writeError != nil {
		fmt.Fprintln(os.Stderr, writeError)
		os.Exit(1)
	}
	fmt.Printf("dedupe complete: in=%s out=%s kept=%d duplicates_removed=%d\n", *inputFile, *outputFile, len(records), duplicates)
}

func dedupe(path string) ([]datasetRecord, int, error) {
	file, openError := os.Open(path)
	if openError != nil {
		return nil, 0, fmt.Errorf("open file: %w", openError)
	}
	defer file.Close()

	records := make([]datasetRecord, 0, 1024)
	seen := map[string]bool{}
	duplicates := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		record := datasetRecord{}
		if unmarshalError := json.Unmarshal([]byte(line), &record); unmarshalError != nil {
			return nil, 0, fmt.Errorf("invalid jsonl record: %w", unmarshalError)
		}
		key := recordIdentity(record)
		if seen[key] {
			duplicates++
			continue
		}
		seen[key] = true
		records = append(records, record)
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, 0, fmt.Errorf("scan file: %w", scanError)
	}
	return records, duplicates, nil
}

func recordIdentity(record datasetRecord) string {
	normalizedInstruction := strings.ToLower(strings.TrimSpace(record.Instruction))
	normalizedInput := compactJSON(record.Input)
	normalizedOutput := compactJSON(record.Output)
	payload := normalizedInstruction + "\n" + normalizedInput + "\n" + normalizedOutput
	sum := sha1.Sum([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func compactJSON(raw string) string {
	buffer := bytes.Buffer{}
	if err := json.Compact(&buffer, []byte(strings.TrimSpace(raw))); err != nil {
		return strings.TrimSpace(raw)
	}
	return buffer.String()
}

func writeDataset(path string, records []datasetRecord) error {
	file, createError := os.Create(path)
	if createError != nil {
		return fmt.Errorf("create output file: %w", createError)
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
		return fmt.Errorf("flush output: %w", flushError)
	}
	return nil
}
