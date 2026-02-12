package security

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type AllowlistMode string

const (
	AllowlistModeOff     AllowlistMode = "off"
	AllowlistModeWarn    AllowlistMode = "warn"
	AllowlistModeEnforce AllowlistMode = "enforce"
)

type Allowlist struct {
	entries []allowlistEntry
}

type allowlistEntry struct {
	kind  string
	value string
	regex *regexp.Regexp
}

func ParseAllowlistMode(value string) (AllowlistMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(AllowlistModeOff):
		return AllowlistModeOff, nil
	case string(AllowlistModeWarn):
		return AllowlistModeWarn, nil
	case string(AllowlistModeEnforce):
		return AllowlistModeEnforce, nil
	default:
		return "", fmt.Errorf("invalid allowlist mode %q (expected off|warn|enforce)", value)
	}
}

func LoadAllowlist(path string) (*Allowlist, error) {
	normalizedPath := strings.TrimSpace(path)
	if normalizedPath == "" {
		return &Allowlist{}, nil
	}

	file, openError := os.Open(normalizedPath)
	if openError != nil {
		return nil, fmt.Errorf("open allowlist file: %w", openError)
	}
	defer file.Close()

	entries := make([]allowlistEntry, 0)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		rawLine := strings.TrimSpace(scanner.Text())
		if rawLine == "" || strings.HasPrefix(rawLine, "#") {
			continue
		}

		entry, parseError := parseAllowlistLine(rawLine)
		if parseError != nil {
			return nil, fmt.Errorf("allowlist line %d: %w", lineNumber, parseError)
		}
		entries = append(entries, entry)
	}
	if scanError := scanner.Err(); scanError != nil {
		return nil, fmt.Errorf("read allowlist file: %w", scanError)
	}

	return &Allowlist{entries: entries}, nil
}

func (allowlist *Allowlist) IsEmpty() bool {
	return allowlist == nil || len(allowlist.entries) == 0
}

func (allowlist *Allowlist) Matches(command string) bool {
	if allowlist == nil {
		return false
	}

	normalizedCommand := strings.TrimSpace(command)
	for _, entry := range allowlist.entries {
		switch entry.kind {
		case "exact":
			if normalizedCommand == entry.value {
				return true
			}
		case "prefix":
			if strings.HasPrefix(normalizedCommand, entry.value) {
				return true
			}
		case "re":
			if entry.regex != nil && entry.regex.MatchString(normalizedCommand) {
				return true
			}
		}
	}
	return false
}

func ValidateAllowlist(command string, allowlist *Allowlist, mode AllowlistMode) (string, error) {
	if mode == AllowlistModeOff {
		return "", nil
	}
	if allowlist == nil || allowlist.IsEmpty() {
		if mode == AllowlistModeWarn {
			return "allowlist warning: allowlist is empty, command was not checked", nil
		}
		return "", fmt.Errorf("allowlist enforcement enabled but allowlist is empty")
	}
	if allowlist.Matches(command) {
		return "", nil
	}

	if mode == AllowlistModeWarn {
		return "allowlist warning: command not found in allowlist", nil
	}
	return "", fmt.Errorf("allowlist blocked: command not found in allowlist")
}

func parseAllowlistLine(line string) (allowlistEntry, error) {
	if strings.HasPrefix(line, "exact:") {
		value := strings.TrimSpace(strings.TrimPrefix(line, "exact:"))
		if value == "" {
			return allowlistEntry{}, fmt.Errorf("empty exact entry")
		}
		return allowlistEntry{kind: "exact", value: value}, nil
	}

	if strings.HasPrefix(line, "prefix:") {
		value := strings.TrimSpace(strings.TrimPrefix(line, "prefix:"))
		if value == "" {
			return allowlistEntry{}, fmt.Errorf("empty prefix entry")
		}
		return allowlistEntry{kind: "prefix", value: value}, nil
	}

	if strings.HasPrefix(line, "re:") {
		value := strings.TrimSpace(strings.TrimPrefix(line, "re:"))
		if value == "" {
			return allowlistEntry{}, fmt.Errorf("empty regex entry")
		}
		compiled, compileError := regexp.Compile(value)
		if compileError != nil {
			return allowlistEntry{}, fmt.Errorf("invalid regex: %w", compileError)
		}
		return allowlistEntry{kind: "re", value: value, regex: compiled}, nil
	}

	return allowlistEntry{kind: "exact", value: line}, nil
}
