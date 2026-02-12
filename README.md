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
curl -fsSL https://example.com/install.sh | sh
```

Windows PowerShell:

```powershell
iwr -useb https://example.com/install.ps1 | iex
```

Example scripts are in `scripts/install.sh` and `scripts/install.ps1`.

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
