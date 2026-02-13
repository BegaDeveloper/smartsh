package main

import (
	"fmt"
	"strings"
	"sync"
)

type metricsRegistry struct {
	mu                  sync.Mutex
	runsTotal           int64
	jobsTotal           int64
	jobsCompleted       int64
	jobsFailed          int64
	jobsBlocked         int64
	runDurationMSTotal  int64
	errorTypeTotals     map[string]int64
}

func newMetricsRegistry() *metricsRegistry {
	return &metricsRegistry{
		errorTypeTotals: map[string]int64{
			"none":       0,
			"compile":    0,
			"test":       0,
			"runtime":    0,
			"dependency": 0,
			"policy":     0,
		},
	}
}

func (metrics *metricsRegistry) recordRun(response runResponse) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.runsTotal++
	metrics.runDurationMSTotal += response.DurationMS
	errorType := strings.TrimSpace(response.ErrorType)
	if errorType == "" {
		errorType = "none"
	}
	metrics.errorTypeTotals[errorType]++
}

func (metrics *metricsRegistry) recordJobStatus(status string) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.jobsTotal++
	switch status {
	case "completed":
		metrics.jobsCompleted++
	case "failed":
		metrics.jobsFailed++
	case "blocked":
		metrics.jobsBlocked++
	}
}

func (metrics *metricsRegistry) renderPrometheus() string {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	lines := []string{
		"# TYPE smartsh_runs_total counter",
		fmt.Sprintf("smartsh_runs_total %d", metrics.runsTotal),
		"# TYPE smartsh_jobs_total counter",
		fmt.Sprintf("smartsh_jobs_total %d", metrics.jobsTotal),
		"# TYPE smartsh_jobs_completed_total counter",
		fmt.Sprintf("smartsh_jobs_completed_total %d", metrics.jobsCompleted),
		"# TYPE smartsh_jobs_failed_total counter",
		fmt.Sprintf("smartsh_jobs_failed_total %d", metrics.jobsFailed),
		"# TYPE smartsh_jobs_blocked_total counter",
		fmt.Sprintf("smartsh_jobs_blocked_total %d", metrics.jobsBlocked),
		"# TYPE smartsh_run_duration_ms_total counter",
		fmt.Sprintf("smartsh_run_duration_ms_total %d", metrics.runDurationMSTotal),
	}
	for key, value := range metrics.errorTypeTotals {
		lines = append(lines, fmt.Sprintf(`smartsh_error_type_total{type="%s"} %d`, key, value))
	}
	return strings.Join(lines, "\n") + "\n"
}
