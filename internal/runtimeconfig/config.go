package runtimeconfig

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type FileConfig struct {
	Path   string
	Values map[string]string
}

func DefaultConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory failed: %w", err)
	}
	return filepath.Join(homeDir, ".smartsh", "config"), nil
}

func Load(path string) (FileConfig, error) {
	configPath := strings.TrimSpace(path)
	if configPath == "" {
		resolvedPath, err := DefaultConfigPath()
		if err != nil {
			return FileConfig{}, err
		}
		configPath = resolvedPath
	}
	values := map[string]string{}
	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return FileConfig{Path: configPath, Values: values}, nil
		}
		return FileConfig{}, fmt.Errorf("open config failed: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			values[key] = value
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return FileConfig{}, fmt.Errorf("read config failed: %w", scanErr)
	}
	return FileConfig{Path: configPath, Values: values}, nil
}

func Save(config FileConfig) error {
	if strings.TrimSpace(config.Path) == "" {
		return fmt.Errorf("config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(config.Path), 0o700); err != nil {
		return fmt.Errorf("create config directory failed: %w", err)
	}
	lines := make([]string, 0, len(config.Values)+1)
	lines = append(lines, "# smartsh runtime config")
	keys := make([]string, 0, len(config.Values))
	for key := range config.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := config.Values[key]
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			continue
		}
		lines = append(lines, normalizedKey+"="+strings.TrimSpace(value))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(config.Path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write config failed: %w", err)
	}
	return nil
}

func ResolveString(key string, defaults map[string]string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if defaults == nil {
		return ""
	}
	return strings.TrimSpace(defaults[key])
}

func ResolveBool(key string, defaults map[string]string) bool {
	raw := ResolveString(key, defaults)
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func EnsureToken(config FileConfig, key string) (FileConfig, string, error) {
	if config.Values == nil {
		config.Values = map[string]string{}
	}
	existing := strings.TrimSpace(config.Values[key])
	if existing != "" {
		return config, existing, nil
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return config, "", fmt.Errorf("generate token failed: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	config.Values[key] = token
	return config, token, nil
}
