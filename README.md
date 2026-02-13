# smartsh

`smartsh` is a cross-platform command-execution gateway written in Go.
It accepts explicit shell commands, applies safety/policy checks, and executes through daemon/MCP integrations.

## Features

- macOS and Windows support
- MCP-first execution flow for Cursor/Claude integrations
- Deterministic safety layer before execution
- Optional command allowlist policy
- Risk approval workflow for dangerous commands

## CLI Modes

- `smartsh mcp` starts the MCP server.
- `smartsh setup-agent` generates integration config files.
- Direct natural-language CLI execution mode has been removed.

## Project Structure

```text
/cmd/smartsh
/internal/security
/internal/mcpserver
/cmd/smartshd
```

## Requirements

- Go 1.23+
- Installed runtimes for commands you want to execute

## Configuration

## Usage

```bash
smartsh mcp
smartsh setup-agent
go run ./cmd/smartshd
```

Shortcut launchers are included:

- `./smsh` (Unix/macOS)
- `./smsh.ps1` (PowerShell)

Typical flow:

1. Tool (Cursor/Claude MCP) sends an explicit command
2. Daemon validates command against safety and allowlist policy
3. Daemon optionally requests approval for risky operations
4. Command executes and returns compact structured summary

## Build

### Local

```bash
go build -o smartsh ./cmd/smartsh
```

### Install via Go (developer-friendly)

```bash
go install github.com/BegaDeveloper/smartsh/cmd/smartsh@latest
go install github.com/BegaDeveloper/smartsh/cmd/smartshd@latest
```

### Cross-platform

Unix/macOS:

```bash
./scripts/build.sh
```

Windows PowerShell:

```powershell
.\scripts\build.ps1
```

Artifacts are produced under `dist/`.
Release archives and checksums are under `dist/release/`.

## Install Examples

Unix/macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/BegaDeveloper/smartsh/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
iwr -useb https://raw.githubusercontent.com/BegaDeveloper/smartsh/main/scripts/install.ps1 | iex
```

Example scripts are in `scripts/install.sh` and `scripts/install.ps1`.

Optional installer configuration:

- `SMARTSH_VERSION`: `latest` (default) or a tag like `v1.2.3`
- `SMARTSH_REPO`: GitHub repo in the form `owner/name` (default: `BegaDeveloper/smartsh`)
- `SMARTSH_INSTALL_DIR`: destination directory (default: `/usr/local/bin` on Unix/macOS)
- `SMARTSH_COMPONENTS`: space/comma separated list (`smartsh` (default) or `smartsh smartshd`)

## Quick Setup (Recommended)

Use one command to bootstrap local agent setup (Ollama check, daemon start, and ready-to-import tool JSON files):

macOS/Linux:

```bash
smartsh setup-agent
```

Windows PowerShell:

```powershell
smartsh.exe setup-agent
```

Script alternatives are still available:

- `./scripts/setup-agent.sh`
- `.\scripts\setup-agent.ps1`

Generated outputs:

- `~/.smartsh/cursor-smartsh-tool.json`
- `~/.smartsh/cursor-smartsh-mcp.json`
- `~/.smartsh/cursor-mcp.json`
- `~/.smartsh/claude-smartsh-tool.json`
- `~/.smartsh/agent-instructions.txt`

For Cursor MCP-native UI:

1. Open `Tools & MCP`
2. Click `New MCP Server`
3. Use values from `~/.smartsh/cursor-smartsh-mcp.json`
4. Paste `~/.smartsh/agent-instructions.txt` into Rules and Commands

If your Cursor supports workspace `mcp.json`, you can also use `~/.smartsh/cursor-mcp.json`.

## Command Execution Mode

Use `smartshd` (directly or through MCP wrappers) with explicit command payloads.

Integration launchers:

- `scripts/integrations/cursor-smartsh.sh`
- `scripts/integrations/claude-smartsh.sh`
- `scripts/integrations/cursor-smartsh.ps1`
- `scripts/integrations/claude-smartsh.ps1`

These wrappers run in the current working directory by default, so commands execute in the active project folder.

## smartshd Local Gateway

Run the local execution gateway:

```bash
go run ./cmd/smartshd
```

Health check:

```bash
curl -sS http://127.0.0.1:8787/health
```

Direct run request:

```bash
curl -sS -X POST http://127.0.0.1:8787/run \
  -H "Content-Type: application/json" \
  -d '{"command":"go test ./...","cwd":"/path/to/project"}'
```

Async run request + polling:

```bash
curl -sS -X POST http://127.0.0.1:8787/run \
  -H "Content-Type: application/json" \
  -d '{"command":"npm test","cwd":"/path/to/project","async":true}'

curl -sS http://127.0.0.1:8787/jobs/<job_id>
```

Response contains compact execution metadata:

- `must_use_smartsh` (strict tool-enforced contract hint)
- `job_id` + `status` (`queued|running|completed|failed|blocked`)
- `executed`
- `resolved_command`
- `exit_code`
- `summary`
- `error_type` (`none|compile|test|runtime|dependency|policy`)
- `top_issues`
- `output_tail`
- `requires_approval` + `approval_id` + `approval_message` + `risk_reason` + `risk_targets`

When `status` is `needs_approval`, confirm with:

```bash
curl -sS -X POST http://127.0.0.1:8787/approvals/<approval_id> \
  -H "Content-Type: application/json" \
  -d '{"approved":true}'
```

### New daemon capabilities

- Persistent jobs in BoltDB (`SMARTSH_DAEMON_DB`) survive restarts
- Async execution with `job_id` + `status`, history listing via `/jobs?limit=`
- SSE status stream via `/jobs/:id/stream`
- PTY interactive sessions (`/sessions`, `/sessions/:id/input`, `/sessions/:id/stream`, `/sessions/:id/close`)
- Execution isolation controls (`timeout_sec`, `max_output_kb`, `max_memory_mb`, `max_cpu_seconds`, env allowlist)
- Deterministic parser-first summarization for Jest/Vitest, Go test, TypeScript, Maven, Gradle, dotnet
- Rich summary fields (`primary_error`, `next_action`, `failing_tests`, `failed_files`)
- Project policy engine via `.smartsh-policy.yaml`
- Optional daemon auth token (`SMARTSH_DAEMON_TOKEN`)
- Prometheus-style metrics at `/metrics`

### API quick reference

- `POST /run`
- `GET /jobs?limit=50`
- `GET /jobs/:id`
- `GET /jobs/:id/stream`
- `GET /approvals/:id`
- `POST /approvals/:id`
- `POST /sessions`
- `GET /sessions/:id`
- `POST /sessions/:id/input`
- `GET /sessions/:id/stream`
- `POST /sessions/:id/close`
- `GET /metrics`

### Example `.smartsh-policy.yaml`

```yaml
version: 1
enforce: true
max_risk: medium
allow_commands:
  - "prefix:npm "
  - "prefix:go "
deny_commands:
  - "re:(?i)rm\\s+-rf\\s+/"
allow_paths:
  - "/Users/you/workspace"
allow_env:
  - "PATH"
  - "HOME"
  - "CI"
```

### Auth

If `SMARTSH_DAEMON_TOKEN` is set, requests must include:

- `X-Smartsh-Token: <token>` header, or
- `Authorization: Bearer <token>`

Wrappers (`scripts/integrations/*`) automatically forward `SMARTSH_DAEMON_TOKEN` when present.

Summary classification is deterministic and parser-first for compact structured diagnostics.

## Cursor / Claude Step-by-Step Test

1. Start daemon:

```bash
go run ./cmd/smartshd
```

2. In another terminal, sanity test:

```bash
./scripts/integrations/cursor-smartsh.sh run this project
```

3. In Cursor or Claude Code tool settings, add a custom tool using:
   - `scripts/integrations/cursor-custom-tool.example.json`
   - or `scripts/integrations/claude-code-tool.example.json`
4. Adjust command path if your local path differs.
5. In agent/system instructions add:
   - "For command execution, always use smartsh_agent tool; do not use direct shell tool."
6. Prompt:
   - "Create this feature and test everything."
7. Confirm tool output returns compact JSON summaries instead of huge raw logs.

Optional async mode in wrappers (auto-polls `/jobs/:id` until terminal state):

```bash
export SMARTSH_ASYNC=1
export SMARTSH_TIMEOUT_SEC=120
export SMARTSH_ALLOWLIST_MODE=warn
export SMARTSH_REQUIRE_APPROVAL=1
export SMARTSH_TERMINAL_SESSION_KEY=cursor-main
./scripts/integrations/cursor-smartsh.sh npm test
```

MCP output compaction (enabled by default, recommended for Cursor token savings):

```bash
export SMARTSH_MCP_COMPACT_OUTPUT=true
export SMARTSH_MCP_MAX_OUTPUT_TAIL_CHARS=600
```

Behavior:

- MCP truncates `output_tail` to configured max chars.

Ready-made config snippets:

- `scripts/integrations/cursor-custom-tool.example.json`
- `scripts/integrations/claude-code-tool.example.json`

Use them as templates in your app settings, then adjust:

- command path (if your repo path differs)
- template variable syntax (if your app uses a different placeholder format)

## Allowlist Mode

Optional command allowlisting can be enabled with:

- `--allowlist-mode off|warn|enforce` (default: `off`)
- `--allowlist-file <path>` (default: `.smartsh-allowlist`)

Mode behavior:

- `off`: no allowlist check
- `warn`: execute but emit warning if command is not allowlisted
- `enforce`: block if command is not allowlisted

Allowlist file format:

- Empty lines and lines starting with `#` are ignored
- `exact:<command>` exact command match
- `prefix:<prefix>` command starts with prefix
- `re:<regex>` regular expression match
- Plain lines are treated as exact matches

Example allowlist is provided in `.smartsh-allowlist.example`.

## Safety Notes

Blocked by default:

- System wipe commands
- Privilege escalation commands
- Pipe-to-shell patterns
- Suspicious destructive commands

Use `--unsafe` only when you understand the consequences.
