# smartsh

`smartsh` is a cross-platform AI-powered CLI tool written in Go.
It takes natural-language input, asks a local Ollama model for structured intent, validates command safety, and executes while streaming output.

## Features

- macOS and Windows support
- Natural-language command requests
- Local Ollama integration (`/api/generate`)
- Strict JSON model contract validation
- Deterministic safety layer before execution
- Runtime and project-type detection
- Interactive confirmation (`--yes` to skip)
- Unsafe override (`--unsafe`) for blocked high-risk operations
- Optional command allowlist policy (`--allowlist-mode`)
- Machine-readable mode (`--json`) for agent/tool integration
- Dry-run mode (`--dry-run`) to resolve/validate without execution
- Debug AI parse mode (`--debug-ai`) to print sanitized raw model output on parse failures

## Model Contract

The model must return exactly:

```json
{
  "intent": "string",
  "command": "string",
  "confidence": 0.0,
  "risk": "low | medium | high"
}
```

Non-JSON or schema-invalid output is rejected.

## Project Structure

```text
/cmd/smartsh
/internal/ai
/internal/security
/internal/detector
/internal/executor
/internal/resolver
```

## Requirements

- Go 1.23+
- Ollama running locally (default `http://localhost:11434/api/generate`)
- Installed runtimes for commands you want to execute

## Configuration

- `SMARTSH_MODEL` - Ollama model name (default: `llama3.1:8b`)
- `SMARTSH_OLLAMA_URL` - Ollama generate endpoint (default: `http://localhost:11434/api/generate`)

## Usage

```bash
smartsh "run this project"
smsh run this project
smartsh --yes "run tests"
smartsh --unsafe "reset this environment"
smartsh --json --yes "build this app"
smartsh --dry-run --yes "build all go packages"
smartsh --allowlist-mode warn --allowlist-file .smartsh-allowlist "run tests"
```

Shortcut launchers are included:

- `./smsh` (Unix/macOS)
- `./smsh.ps1` (PowerShell)

Typical flow:

1. Detect environment (OS, project files, runtimes)
2. Ask AI for strict JSON intent
3. Resolve/validate command via deterministic safety layer
4. Print resolved command
5. Confirm execution (unless `--yes` or `--json`)
6. Execute and stream output

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

## JSON Agent Mode

`smartsh --json` outputs machine-readable execution metadata:

```json
{
  "executed": true,
  "resolved_command": "go test ./...",
  "exit_code": 0,
  "intent": "run tests",
  "confidence": "0.93",
  "risk": "low"
}
```

This is intended for tool integrations where another system orchestrates command execution decisions.

### Agent Input Mode (Cursor / Claude)

Use `--agent` to simplify automation:

- Forces JSON output (`--json`)
- Auto-confirm enabled (`--yes`)
- Accepts instruction from args **or** stdin
- Accepts JSON request payload via stdin

Examples:

```bash
smartsh --agent run tests
echo "build this project" | smartsh --agent
echo '{"instruction":"run tests","cwd":"/path/to/project","dry_run":true}' | smartsh --agent
```

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
  -d '{"instruction":"run tests","cwd":"/path/to/project"}'
```

Async run request + polling:

```bash
curl -sS -X POST http://127.0.0.1:8787/run \
  -H "Content-Type: application/json" \
  -d '{"instruction":"npm test","cwd":"/path/to/project","async":true}'

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

Summary classification is heuristic-first and can be Ollama-assisted for richer `error_type` and `top_issues`.
To disable Ollama-assisted summarization, set:

```bash
export SMARTSH_SUMMARY_ENABLED=false
```

Optional summarization model override:

```bash
export SMARTSH_SUMMARY_MODEL=llama3.1:8b
```

For local debugging of summarization behavior (prompt + raw Ollama output + parsed result), set:

```bash
export SMARTSH_SUMMARY_DEBUG_DIR=/tmp/smartsh-summary-debug
```

Each failed summarized run writes a timestamped folder with:

- `01_prompt.txt`
- `02_raw_ollama_body.json`
- `03_raw_ollama_response.txt`
- `04_parsed_ollama_summary.json`
- `05_final_summary_used.json`

## Cursor / Claude Step-by-Step Test

1. Start Ollama and ensure model is available.
2. Start daemon:

```bash
go run ./cmd/smartshd
```

3. In another terminal, sanity test:

```bash
./scripts/integrations/cursor-smartsh.sh run this project
```

4. In Cursor or Claude Code tool settings, add a custom tool using:
   - `scripts/integrations/cursor-custom-tool.example.json`
   - or `scripts/integrations/claude-code-tool.example.json`
5. Adjust command path if your local path differs.
6. In agent/system instructions add:
   - "For command execution, always use smartsh_agent tool; do not use direct shell tool."
7. Prompt:
   - "Create this feature and test everything."
8. Confirm tool output returns compact JSON summaries instead of huge raw logs.

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

- If `summary_source=ollama`, MCP omits `output_tail`.
- Otherwise MCP truncates `output_tail` to configured max chars.

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

## Training Starter Pack

Starter files for model adaptation are in `training/`:

- `training/dataset.schema.md`
- `training/smartsh_train.jsonl`
- `training/modelfile.example`

Validate training data before use:

```bash
go run ./scripts/validate-training-data --file ./training/smartsh_train.jsonl
go run ./scripts/score-training-data --file ./training/smartsh_train.jsonl
go run ./scripts/dedupe-training-data --file ./training/smartsh_train.jsonl --out ./training/smartsh_train.deduped.jsonl
```

Recommended workflow:

1. Expand `training/smartsh_train.jsonl` with your domain-specific examples.
2. Keep `output` strict to the `smartsh` schema (`intent`, `command`, `confidence`, `risk`).
3. Fine-tune externally (LoRA/QLoRA toolchain) and serve result via Ollama.
4. Keep `smartsh` deterministic safety layer enabled regardless of model quality.

## Safety Notes

Blocked by default:

- System wipe commands
- Privilege escalation commands
- Pipe-to-shell patterns
- Suspicious destructive commands

Use `--unsafe` only when you understand the consequences.
