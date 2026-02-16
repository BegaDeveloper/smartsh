<div align="center">

# >smartsh_

**Safe, compact command execution for AI coding agents.**

[![Cursor](https://img.shields.io/badge/Cursor-MCP%20Ready-6C47FF?style=flat&logo=data:image/svg+xml;base64,PHN2ZyB3aWR0aD0iMjQiIGhlaWdodD0iMjQiIHZpZXdCb3g9IjAgMCAyNCAyNCIgZmlsbD0ibm9uZSIgeG1sbnM9Imh0dHA6Ly93d3cudzMub3JnLzIwMDAvc3ZnIj48cGF0aCBkPSJNMTIgMkw0IDdMMTIgMTJMMjAgN0wxMiAyWiIgZmlsbD0id2hpdGUiLz48L3N2Zz4=&logoColor=white)](https://cursor.com)
[![Claude Code](https://img.shields.io/badge/Claude%20Code-Compatible-D97706?style=flat&logo=anthropic&logoColor=white)](https://claude.ai)
[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)

[![macOS](https://img.shields.io/badge/macOS-000000?style=flat&logo=apple&logoColor=white)]()
[![Linux](https://img.shields.io/badge/Linux-FCC624?style=flat&logo=linux&logoColor=000)]()
[![Windows 11 Pro](https://img.shields.io/badge/Windows%2011%20Pro-005FB8?style=flat&logo=windows&logoColor=white)]()

MCP server + local daemon that gives **Cursor** and **Claude Code** a safe, token-efficient way to run shell commands.

</div>

---

## Why smartsh?

When AI agents run terminal commands, they dump **huge raw logs** into context — burning tokens and confusing the model.

**smartsh** fixes this:

- Runs commands through a local daemon (`smartshd`)
- Returns **compact structured JSON** instead of raw output
- Applies **safety checks** before execution (blocks dangerous commands)
- Supports **risk approval workflows** for destructive operations
- Truncates output automatically for **massive token savings**

---

## Quick Install (End Users)

### macOS / Linux (one command)

```bash
curl -fsSL https://raw.githubusercontent.com/BegaDeveloper/smartsh/main/scripts/install.sh \
  | sh \
  && smartsh setup-agent
```

> Auto-detects OS/CPU, downloads from latest release, verifies checksum, installs `smartsh` + `smartshd`, then `setup-agent` generates Cursor/Claude config files in `~/.smartsh/`.

### Required: Ollama (always-on summaries)

smartsh now defaults to Ollama-backed summaries. Install Ollama and pull the model:

```bash
# macOS / Linux
curl -fsSL https://ollama.com/install.sh | sh
ollama serve
ollama pull llama3.2:3b
```

Windows:

- Install Ollama from `https://ollama.com/download`
- Then run:

```powershell
ollama serve
ollama pull llama3.2:3b
```

`smartsh setup-agent` now performs a strict preflight:

- verifies Ollama is reachable at `SMARTSH_OLLAMA_URL` (default `http://127.0.0.1:11434`)
- verifies `SMARTSH_OLLAMA_MODEL` exists locally (default `llama3.2:3b`)
- fails fast with instructions if either check is missing

### Windows (PowerShell)

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -Command "iwr -useb https://raw.githubusercontent.com/BegaDeveloper/smartsh/main/scripts/install.ps1 | iex; smartsh setup-agent"
```

### Install via Go

```bash
go install github.com/BegaDeveloper/smartsh/cmd/smartsh@latest
go install github.com/BegaDeveloper/smartsh/cmd/smartshd@latest
smartsh setup-agent
```

### Manual download

Grab the right archive from [**Releases**](https://github.com/BegaDeveloper/smartsh/releases):

| Platform | File |
|---|---|
| macOS Apple Silicon (M1/M2/M3) | `smartsh_darwin_arm64.tar.gz` |
| macOS Intel | `smartsh_darwin_amd64.tar.gz` |
| Linux x64 | `smartsh_linux_amd64.tar.gz` |
| Linux arm64 | `smartsh_linux_arm64.tar.gz` |
| Windows x64 | `smartsh_windows_amd64.zip` |

Direct latest asset URLs:

```text
https://github.com/BegaDeveloper/smartsh/releases/latest/download/smartsh_darwin_arm64.tar.gz
https://github.com/BegaDeveloper/smartsh/releases/latest/download/smartsh_darwin_amd64.tar.gz
https://github.com/BegaDeveloper/smartsh/releases/latest/download/smartsh_linux_amd64.tar.gz
https://github.com/BegaDeveloper/smartsh/releases/latest/download/smartsh_linux_arm64.tar.gz
https://github.com/BegaDeveloper/smartsh/releases/latest/download/smartsh_windows_amd64.zip
```

---

## Setup with Cursor

After install, run:

```bash
smartsh setup-agent
```

This generates ready-to-use config files in `~/.smartsh/`:

| File | Purpose |
|---|---|
| `cursor-smartsh-mcp.json` | MCP server config for Cursor |
| `cursor-mcp.json` | Workspace `mcp.json` format |
| `claude-smartsh-tool.json` | Claude Code tool config |
| `agent-instructions.txt` | Paste into Cursor rules |
| `config` | Runtime config (includes generated `SMARTSH_DAEMON_TOKEN`) |

Validate your local setup any time with:

```bash
smartsh doctor
```

`smartsh doctor` checks:
- daemon auth/token
- daemon health endpoint
- ollama health + configured model presence
- generated Cursor/Claude MCP config files

### Connect to Cursor

1. Open **Cursor** → **Settings** → **Tools & MCP**
2. Click **New MCP Server**
3. Use values from `~/.smartsh/cursor-smartsh-mcp.json`
4. Paste `~/.smartsh/agent-instructions.txt` into **Rules**

**Or** drop `~/.smartsh/cursor-mcp.json` into your project as `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "smartsh": {
      "command": "smartsh",
      "args": ["mcp"],
      "env": {
        "SMARTSH_DAEMON_URL": "http://127.0.0.1:8787",
        "SMARTSH_DAEMON_TOKEN": "<token-from-~/.smartsh/config>",
        "SMARTSH_MCP_COMPACT_OUTPUT": "true",
        "SMARTSH_MCP_MAX_OUTPUT_TAIL_CHARS": "400",
        "SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL": "false",
        "SMARTSH_SUMMARY_PROVIDER": "ollama",
        "SMARTSH_OLLAMA_REQUIRED": "true",
        "SMARTSH_OLLAMA_URL": "http://127.0.0.1:11434",
        "SMARTSH_OLLAMA_MODEL": "llama3.2:3b"
      }
    }
  }
}
```

### Connect to Claude Code

Use `~/.smartsh/claude-smartsh-tool.json` or add MCP server manually with command `smartsh` and arg `mcp`.

### Rule snippet (recommended)

Add this in your Cursor/Claude rules:

```text
For command execution, always use smartsh_run (or smartsh-local_smartsh_run / smartsh_agent).
Default to open_external_terminal=false for speed.
Use open_external_terminal=true only for interactive/watch/TUI commands.
Never use direct shell unless explicitly requested.
```

---

## How It Works

```
┌─────────────┐     MCP JSON-RPC      ┌─────────┐     HTTP      ┌──────────┐
│ Cursor /     │ ──────────────────▶  │ smartsh  │ ──────────▶  │ smartshd │
│ Claude Code  │ ◀──────────────────  │   mcp    │ ◀──────────  │  daemon  │
└─────────────┘   compact summary     └─────────┘   execute     └──────────┘
```

1. **Agent sends command** via MCP tool (`smartsh_run`)
2. **`smartsh mcp`** forwards to local daemon
3. **`smartshd`** validates safety → executes → summarizes output
4. **Compact JSON** returned to agent (not raw logs)

### Example response

```json
{
  "status": "failed",
  "exit_code": 1,
  "summary": "command failed (exit code 1): Cannot find module '@app/auth'",
  "error_type": "compile",
  "primary_error": "Cannot find module '@app/auth'",
  "next_action": "Fix TypeScript compiler errors and rerun build/test.",
  "failed_files": ["src/app/auth/auth.service.ts"],
  "top_issues": ["TS2307: Cannot find module '@app/auth'"]
}
```

Compare that to 500+ lines of raw `tsc` output the agent would normally dump.

---

## Features

### Safety & Policy

- Blocks dangerous commands (`rm -rf /`, privilege escalation, pipe-to-shell)
- Risk approval workflow — agent must confirm before running destructive ops
- Command allowlist mode (`off` / `warn` / `enforce`)
- Project-level policy via `.smartsh-policy.yaml`

### Token Savings

- Success runs return **only summary** (no output tail)
- Failed runs return **truncated tail** + structured error info
- MCP compact mode enabled by default
- Configurable tail size via `SMARTSH_MCP_MAX_OUTPUT_TAIL_CHARS`

### Smart Summarization

Ollama-backed summaries are default (`SMARTSH_SUMMARY_PROVIDER=ollama`), with deterministic parsing still available as fallback/override.

Deterministic parsers support:

- Jest / Vitest test output
- Go test failures
- TypeScript compiler errors
- Maven / Gradle build failures
- .NET build and test output

### Daemon Capabilities

- Persistent jobs in BoltDB (survive restarts)
- Async execution with `job_id` polling
- SSE status streaming
- PTY interactive sessions
- Execution isolation (timeout, memory, CPU, env allowlist)
- Token auth is required by default (`SMARTSH_DAEMON_TOKEN`)
- Prometheus metrics at `/metrics`

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `SMARTSH_DAEMON_URL` | `http://127.0.0.1:8787` | Daemon address |
| `SMARTSH_DAEMON_TOKEN` | *(auto-generated via `setup-agent`)* | Auth token for daemon requests (required unless auth is disabled) |
| `SMARTSH_DAEMON_DISABLE_AUTH` | `false` | Disable daemon auth checks explicitly (not recommended) |
| `SMARTSH_MCP_COMPACT_OUTPUT` | `true` | Enable compact MCP responses |
| `SMARTSH_MCP_DEFAULT_ALLOWLIST_MODE` | `warn` | Default allowlist mode when client does not set one |
| `SMARTSH_MCP_MAX_OUTPUT_TAIL_CHARS` | `600` | Max chars in output tail |
| `SMARTSH_MCP_OPEN_EXTERNAL_TERMINAL` | `false` | Open external terminal for runs (`true` for interactive/TUI/watch tasks) |
| `SMARTSH_DAEMON_ADDR` | `127.0.0.1:8787` | Daemon listen address |
| `SMARTSH_DAEMON_DB` | *(auto)* | BoltDB path for job persistence |
| `SMARTSH_SUMMARY_PROVIDER` | `ollama` | Summary provider (`deterministic`, `ollama`, `hybrid`) |
| `SMARTSH_OLLAMA_REQUIRED` | `true` | Require Ollama summaries; if unavailable response is marked `ollama_unavailable` |
| `SMARTSH_OLLAMA_URL` | `http://127.0.0.1:11434` | Ollama endpoint for model summaries |
| `SMARTSH_OLLAMA_MODEL` | `llama3.2:3b` | Ollama model used for summaries |
| `SMARTSH_OLLAMA_TIMEOUT_SEC` | `8` | Timeout for Ollama summary request |
| `SMARTSH_OLLAMA_MAX_INPUT_CHARS` | `3500` | Max output chars sent to Ollama |

### Policy File (`.smartsh-policy.yaml`)

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

### Allowlist File (`.smartsh-allowlist`)

```text
# Exact match
exact:npm test

# Prefix match
prefix:go test

# Regex match
re:^npm run (build|dev|test)$
```

---

## API Reference

### Daemon endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Health check |
| `POST` | `/run` | Execute a command |
| `GET` | `/jobs` | List recent jobs |
| `GET` | `/jobs/:id` | Get job status |
| `GET` | `/jobs/:id/stream` | SSE job status stream |
| `POST` | `/approvals/:id` | Approve/reject risky command |
| `POST` | `/sessions` | Create PTY session |
| `GET` | `/sessions/:id` | Get session status |
| `POST` | `/sessions/:id/input` | Send input to session |
| `GET` | `/sessions/:id/stream` | SSE session output stream |
| `POST` | `/sessions/:id/close` | Close session |
| `GET` | `/metrics` | Prometheus metrics |

### MCP tools

| Tool | Description |
|---|---|
| `smartsh_run` | Execute command through daemon |
| `smartsh_approve` | Approve/reject pending risky command |

---

## Building from Source

```bash
# Local build
go build -o smartsh ./cmd/smartsh
go build -o smartshd ./cmd/smartshd

# Cross-platform (produces dist/release/*.tar.gz + *.zip)
./scripts/build.sh        # macOS/Linux
.\scripts\build.ps1       # Windows
```

---

## Release Flow (Maintainers)

You usually do **not** need to manually upload release files.

This repository already has a release workflow:

- GitHub Action: `.github/workflows/release.yml`
- Trigger: push a tag matching `v*`
- Publisher: GoReleaser (`.goreleaser.yaml`)

### Standard release process

```bash
# 1) ensure everything is pushed on main
git checkout main
git pull

# 2) optional local build/test check
go test ./...
./scripts/build.sh

# 3) create and push a version tag
git tag v0.1.1
git push origin v0.1.1
```

After tag push, GitHub Actions should build artifacts and publish the GitHub Release automatically.

### Manual fallback (if needed)

```bash
# Build archives + checksums locally
./scripts/build.sh

# Artifacts are in:
# dist/release/
```

Then upload files from `dist/release/` to a GitHub Release manually.

CLI fallback:

```bash
gh release create v0.1.1 \
  dist/release/smartsh_darwin_amd64.tar.gz \
  dist/release/smartsh_darwin_arm64.tar.gz \
  dist/release/smartsh_linux_amd64.tar.gz \
  dist/release/smartsh_linux_arm64.tar.gz \
  dist/release/smartsh_windows_amd64.zip \
  dist/release/checksums.txt \
  --title v0.1.1 \
  --generate-notes
```

---

## Service Install

Install and start `smartshd` as a user service:

```bash
smartshd install-service
```

- macOS: installs a `launchd` agent
- Linux: installs a `systemd --user` service
- Windows: installs a Task Scheduler task

---

## Project Structure

```text
cmd/smartsh/          CLI entry point (MCP server + setup-agent)
cmd/smartshd/         Local execution daemon
internal/mcpserver/   MCP JSON-RPC server implementation
internal/security/    Safety checks, allowlist, command assessment
internal/setupagent/  Config file generator for Cursor/Claude
scripts/
  build.sh            Cross-platform build script
  install.sh          Auto-detect installer (macOS/Linux)
  install.ps1         Installer (Windows)
  integrations/       Wrapper scripts for Cursor/Claude
```

---

## Safety Notes

Blocked by default:
- System wipe commands
- Privilege escalation
- Pipe-to-shell patterns
- Recursive destructive operations

The `--unsafe` flag or `require_approval` workflow is needed for risky operations. The agent must explicitly confirm.

For Cursor/Claude MCP usage, prefer approval flow:

- first call returns `status=needs_approval` with `approval_id`
- then call `smartsh_approve` with `decision=yes` or `decision=no`

Use `unsafe=true` only when you intentionally want to bypass interactive approval for risky commands.

---

<div align="center">

**[Releases](https://github.com/BegaDeveloper/smartsh/releases)** · **[Issues](https://github.com/BegaDeveloper/smartsh/issues)** · **[License](LICENSE)**

Built with Go. Made for AI-assisted development.

</div>
