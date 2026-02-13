# smartsh Testing Guide

This guide covers both:

- developer-level verification (automated tests),
- user-level verification (Cursor + MCP behavior).

## 1) Quick developer checks

Run from the repository root:

```bash
go test ./...
```

Focused suites:

```bash
go test ./cmd/smartshd
go test ./internal/mcpserver
```

What these tests validate:

- summary parsing and classification,
- output size/tail behavior for token control,
- `summary_source` behavior (`deterministic` vs `ollama`),
- MCP async polling behavior for long-running jobs.

## 2) Start and verify daemon

Start daemon:

```bash
go run ./cmd/smartshd
```

Health check:

```bash
curl -sS http://127.0.0.1:8787/health
```

Expected response contains:

- `"ok": true`
- `"must_use_smartsh": true`

## 3) User-perspective test in Cursor chat

Use prompts that explicitly force smartsh MCP tool usage:

- `Use only smartsh-local_smartsh_run. Do not use direct shell. Run go test ./... in /Applications/smartsh.`
- `Use only smartsh-local_smartsh_run. Run go test ./cmd/smartshd/does-not-exist in /Applications/smartsh.`
- `Use only smartsh-local_smartsh_run and return the raw tool JSON.`

### What to look for in tool output

- `executed: true`
- `status` (`completed`/`failed`/`blocked`/`running`)
- `exit_code` (source of truth for pass/fail)
- `summary` and `error_type`
- `summary_source`:
  - `deterministic` for successful runs,
  - usually `ollama` for failed runs when Ollama is reachable.

Token-control expectations:

- success runs should not include `output_tail`,
- failed runs include a short `output_tail`,
- output is summarized into compact JSON instead of raw logs.

## 4) Direct API check (optional)

Success case:

```bash
curl -sS -X POST http://127.0.0.1:8787/run \
  -H "Content-Type: application/json" \
  -d '{"command":"go test ./...","cwd":"/Applications/smartsh","async":false,"timeout_sec":180,"unsafe":true}'
```

Failure case:

```bash
curl -sS -X POST http://127.0.0.1:8787/run \
  -H "Content-Type: application/json" \
  -d '{"command":"go test ./cmd/smartshd/does-not-exist","cwd":"/Applications/smartsh","async":false,"timeout_sec":180,"unsafe":true}'
```

## 5) Troubleshooting checklist

- If output format looks old, restart daemon (`smartshd`) and reload Cursor MCP server.
- If `summary_source` is always `deterministic` on failures, verify Ollama is running:
  - `curl -sS http://127.0.0.1:11434/api/tags`
- If tool calls appear stuck/cancelled, use smaller `mcp_max_wait_sec` and poll via `job_id`.
- If Cursor runs direct shell instead of MCP, enforce instruction:
  - `Use only smartsh-local_smartsh_run for command execution.`
