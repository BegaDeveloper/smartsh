package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	store, err := newJobStore(dbPathFromEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "smartshd failed to open job store: %v\n", err)
		os.Exit(1)
	}
	defer store.Close()

	server := newDaemonServer(store)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/run", server.handleRun)
	mux.HandleFunc("/jobs", server.handleJobs)
	mux.HandleFunc("/jobs/", server.handleJobRoutes)
	mux.HandleFunc("/approvals/", server.handleApprovalRoutes)
	mux.HandleFunc("/sessions", server.handleSessions)
	mux.HandleFunc("/sessions/", server.handleSessionRoutes)
	mux.HandleFunc("/metrics", server.handleMetrics)

	address := strings.TrimSpace(os.Getenv("SMARTSH_DAEMON_ADDR"))
	if address == "" {
		address = "127.0.0.1:8787"
	}

	httpServer := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	fmt.Printf("smartshd listening on http://%s\n", address)
	if serveError := httpServer.ListenAndServe(); serveError != nil && serveError != http.ErrServerClosed {
		fmt.Fprintf(os.Stderr, "smartshd failed: %v\n", serveError)
		os.Exit(1)
	}
}
