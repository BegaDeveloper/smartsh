package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

type ptyCreateRequest struct {
	Instruction string            `json:"instruction,omitempty"`
	Command     string            `json:"command,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	TimeoutSec  int               `json:"timeout_sec,omitempty"`
	Unsafe      bool              `json:"unsafe,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

type ptySession struct {
	ID              string
	Command         string
	Cwd             string
	Status          string
	ExitCode        int
	StartedAt       time.Time
	UpdatedAt       time.Time
	OutputTail      string
	ResolvedSummary string
	file            *os.File
	cmd             *exec.Cmd
	cancel          context.CancelFunc
	mu              sync.Mutex
	subscribers     map[chan string]struct{}
}

func (server *daemonServer) createPTYSession(ctx context.Context, requestPayload ptyCreateRequest) (map[string]any, int, error) {
	if runtime.GOOS == "windows" {
		return nil, 400, fmt.Errorf("interactive PTY is not supported on windows in this build")
	}
	cwd, err := resolveWorkingDirectory(requestPayload.Cwd)
	if err != nil {
		return nil, 400, err
	}

	command := strings.TrimSpace(requestPayload.Command)
	if command == "" {
		command = strings.TrimSpace(requestPayload.Instruction)
	}
	if command == "" {
		return nil, 400, fmt.Errorf("command or instruction is required")
	}

	_ = ctx
	sessionCtx, cancel := context.WithCancel(context.Background())
	if requestPayload.TimeoutSec > 0 {
		sessionCtx, cancel = context.WithTimeout(context.Background(), time.Duration(requestPayload.TimeoutSec)*time.Second)
	}
	var execCommand *exec.Cmd
	if runtime.GOOS == "windows" {
		execCommand = exec.CommandContext(sessionCtx, "cmd", "/C", command)
	} else {
		execCommand = exec.CommandContext(sessionCtx, "sh", "-c", command)
	}
	execCommand.Dir = cwd

	ptyFile, err := pty.Start(execCommand)
	if err != nil {
		cancel()
		return nil, 500, err
	}

	sessionID := fmt.Sprintf("pty_%d", time.Now().UnixNano())
	session := &ptySession{
		ID:          sessionID,
		Command:     command,
		Cwd:         cwd,
		Status:      "running",
		ExitCode:    0,
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		file:        ptyFile,
		cmd:         execCommand,
		cancel:      cancel,
		subscribers: map[chan string]struct{}{},
	}

	server.ptySessionsMutex.Lock()
	server.ptySessions[sessionID] = session
	server.ptySessionsMutex.Unlock()

	go server.consumePTYOutput(session)
	return map[string]any{
		"must_use_smartsh": true,
		"session_id":       sessionID,
		"status":           "running",
		"resolved_command": command,
	}, 200, nil
}

func (server *daemonServer) consumePTYOutput(session *ptySession) {
	reader := bufio.NewReader(session.file)
	buffer := make([]byte, 512)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			session.mu.Lock()
			session.OutputTail = tailString(session.OutputTail+chunk, 8000)
			for subscriber := range session.subscribers {
				select {
				case subscriber <- chunk:
				default:
				}
			}
			session.UpdatedAt = time.Now()
			session.mu.Unlock()
		}
		if err != nil {
			break
		}
	}

	exitCode := 0
	waitErr := session.cmd.Wait()
	if waitErr != nil {
		exitCode = 1
	}
	session.mu.Lock()
	session.ExitCode = exitCode
	if exitCode == 0 {
		session.Status = "completed"
		session.ResolvedSummary = "interactive session completed successfully"
	} else {
		session.Status = "failed"
		session.ResolvedSummary = "interactive session failed"
	}
	session.UpdatedAt = time.Now()
	for subscriber := range session.subscribers {
		close(subscriber)
	}
	session.subscribers = map[chan string]struct{}{}
	_ = session.file.Close()
	session.mu.Unlock()
}
