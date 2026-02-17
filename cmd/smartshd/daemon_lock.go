package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type daemonLock struct {
	path string
}

func acquireDaemonLock() (*daemonLock, error) {
	lockPath, err := daemonLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory failed: %w", err)
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			if !isDaemonLikelyRunning() {
				_ = os.Remove(lockPath)
				file, retryErr := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if retryErr == nil {
					defer file.Close()
					_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
					return &daemonLock{path: lockPath}, nil
				}
			}
			return nil, fmt.Errorf("daemon lock already exists at %s (another smartshd may be running)", lockPath)
		}
		return nil, fmt.Errorf("create daemon lock failed: %w", err)
	}
	defer file.Close()
	_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
	return &daemonLock{path: lockPath}, nil
}

func daemonLockPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory failed: %w", err)
	}
	return filepath.Join(homeDir, ".smartsh", "smartshd.lock"), nil
}

func (lock *daemonLock) release() {
	if lock == nil {
		return
	}
	_ = os.Remove(lock.path)
}

func isDaemonLikelyRunning() bool {
	address := strings.TrimSpace(os.Getenv("SMARTSH_DAEMON_ADDR"))
	if address == "" {
		address = "127.0.0.1:8787"
	}
	url := "http://" + address + "/health"
	client := &http.Client{Timeout: 800 * time.Millisecond}
	request, requestErr := http.NewRequest(http.MethodGet, url, nil)
	if requestErr != nil {
		return false
	}
	response, responseErr := client.Do(request)
	if responseErr != nil {
		return false
	}
	defer response.Body.Close()
	// Any HTTP response means another daemon is already bound and serving.
	return true
}
