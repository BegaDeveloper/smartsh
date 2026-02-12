
You are a senior systems engineer.

Build an MVP of a cross-platform AI-powered CLI tool written in Go.

# Goal

Create a CLI tool called `smartsh` that:

* Runs on macOS and Windows
* Accepts natural language input
* Uses a local Ollama model
* Converts intent into structured JSON
* Safely maps that intent into real system commands
* Executes them
* Streams output
* Can be installed via curl (Unix) or PowerShell (Windows)

This tool must work with ANY language ecosystem:

* Node.js
* .NET
* Java
* Python
* C/C++
* Go
* Docker
* Generic system commands

Do NOT limit logic to package.json or predefined scripts.
The AI decides intent, but execution must pass through a deterministic safety layer.

---

# Core Design Rules

1. NEVER execute raw model output directly.
2. The model must return strict JSON.
3. The CLI validates and maps intent to commands.
4. Dangerous patterns must be blocked.
5. Must work outside of project folders as well.

---

# Ollama

Use:
[http://localhost:11434/api/generate](http://localhost:11434/api/generate)

Model configurable via env variable.

Force model to return structured JSON:

{
"intent": string,
"command": string,
"confidence": number,
"risk": "low | medium | high"
}

Reject non-JSON output.

---

# Execution Layer

Implement:

* OS detection
* Language/project detection (heuristics: file scanning)
* Runtime detection (node, dotnet, python, java, gcc, docker etc.)
* Safe command validator
* Optional `--unsafe` flag
* Optional `--yes` auto-confirm flag
* Streaming output
* Proper exit codes
* Signal handling

Block:

* system wipe commands
* privilege escalation
* pipe-to-shell patterns
* suspicious destructive commands unless --unsafe

---

# AI Agent Mode

Add:

smartsh --json

Returns structured machine-readable output so tools like Cursor or Claude can call it programmatically.
I want to save tokens and similar for users using cursor or claude code, so it should integrate with cursor or claude code so when it needs to run commands it calls our terminal !
Example:

{
"executed": true,
"resolved_command": "...",
"exit_code": 0
}

---

# Architecture

Use clean modular structure:

/cmd
/internal/ai
/internal/security
/internal/detector
/internal/executor

Write idiomatic, production-grade Go.

---

# UX

When user types:

smartsh "run this project"

The tool:

1. Detects environment
2. Asks AI for structured intent
3. Resolves to safe command
4. Prints resolved command
5. Asks for confirmation
6. Executes
7. Streams output

---

# Deliverables

* Full working MVP
* Build instructions
* Cross-platform build scripts
* Installer script examples
* README

Do not write pseudo-code.
Write complete implementation.

---

# End

---

