package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BegaDeveloper/smartsh/internal/mcpserver"
	"github.com/BegaDeveloper/smartsh/internal/setupagent"
)

const (
	exitSuccess = 0
	exitFailure = 1
)

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) == "setup-agent" {
		if setupError := setupagent.Run(os.Stdout); setupError != nil {
			fmt.Fprintf(os.Stderr, "setup-agent failed: %v\n", setupError)
			return exitFailure
		}
		return exitSuccess
	}
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) == "mcp" {
		if serverError := mcpserver.Run(); serverError != nil {
			fmt.Fprintf(os.Stderr, "mcp server failed: %v\n", serverError)
			return exitFailure
		}
		return exitSuccess
	}

	fmt.Fprintln(os.Stderr, "smart terminal mode was removed. use: smartsh mcp")
	return exitFailure
}
