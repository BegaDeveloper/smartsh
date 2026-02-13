#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
OUT_DIR="${SMARTSH_SETUP_OUT_DIR:-$HOME/.smartsh}"
DAEMON_URL="${SMARTSH_DAEMON_URL:-http://127.0.0.1:8787}"

mkdir -p "$OUT_DIR"

info() {
  printf "%s\n" "$1"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1"
    exit 1
  fi
}

ensure_daemon() {
  if curl -sS --max-time 1 "$DAEMON_URL/health" >/dev/null 2>&1; then
    return 0
  fi
  if command -v smartshd >/dev/null 2>&1; then
    nohup smartshd >/tmp/smartshd.log 2>&1 &
  else
    nohup go run "$ROOT_DIR/cmd/smartshd" >/tmp/smartshd.log 2>&1 &
  fi
  tries=0
  while [ "$tries" -lt 30 ]; do
    if curl -sS --max-time 1 "$DAEMON_URL/health" >/dev/null 2>&1; then
      return 0
    fi
    tries=$((tries + 1))
    sleep 0.2
  done
  echo "smartshd failed to start"
  exit 1
}

write_cursor_tool() {
  cursor_file="$OUT_DIR/cursor-smartsh-tool.json"
  cat >"$cursor_file" <<EOF
{
  "name": "smartsh-agent",
  "description": "Run terminal commands through smartshd and return compact summaries.",
  "command": "$ROOT_DIR/scripts/integrations/cursor-smartsh.sh",
  "args": [],
  "inputSchema": {
    "type": "object",
    "properties": {
      "command": { "type": "string" },
      "cwd": { "type": "string" },
      "dry_run": { "type": "boolean" },
      "unsafe": { "type": "boolean" },
      "async": { "type": "boolean" },
      "timeout_sec": { "type": "integer" },
      "allowlist_mode": { "type": "string", "enum": ["off", "warn", "enforce"] },
      "allowlist_file": { "type": "string" }
    },
    "required": ["command"]
  },
  "stdinTemplate": "{\"command\":\"{{command}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\"}"
}
EOF
}

write_claude_tool() {
  claude_file="$OUT_DIR/claude-smartsh-tool.json"
  cat >"$claude_file" <<EOF
{
  "tools": [
    {
      "name": "smartsh_agent",
      "description": "Execute terminal commands through smartshd and return compact summaries.",
      "command": "$ROOT_DIR/scripts/integrations/claude-smartsh.sh",
      "args": [],
      "input_schema": {
        "type": "object",
        "properties": {
          "command": { "type": "string" },
          "cwd": { "type": "string" },
          "dry_run": { "type": "boolean" },
          "unsafe": { "type": "boolean" },
          "async": { "type": "boolean" },
          "timeout_sec": { "type": "integer" },
          "allowlist_mode": { "type": "string", "enum": ["off", "warn", "enforce"] },
          "allowlist_file": { "type": "string" }
        },
        "required": ["command"]
      },
      "stdin_template": "{\"command\":\"{{command}}\",\"cwd\":\"{{cwd}}\",\"dry_run\":{{dry_run}},\"unsafe\":{{unsafe}},\"async\":{{async}},\"timeout_sec\":{{timeout_sec}},\"allowlist_mode\":\"{{allowlist_mode}}\",\"allowlist_file\":\"{{allowlist_file}}\"}"
    }
  ]
}

write_cursor_mcp() {
  mcp_file="$OUT_DIR/cursor-smartsh-mcp.json"
  mcp_script="$ROOT_DIR/scripts/integrations/smartsh-mcp.sh"
  cat >"$mcp_file" <<EOF
{
  "name": "smartsh",
  "command": "/bin/sh",
  "args": ["$mcp_script"],
  "env": {
    "SMARTSH_DAEMON_URL": "$DAEMON_URL"
  }
}
EOF
}

write_cursor_mcp_workspace() {
  mcp_workspace_file="$OUT_DIR/cursor-mcp.json"
  mcp_script="$ROOT_DIR/scripts/integrations/smartsh-mcp.sh"
  cat >"$mcp_workspace_file" <<EOF
{
  "mcpServers": {
    "smartsh": {
      "command": "/bin/sh",
      "args": ["$mcp_script"],
      "env": {
        "SMARTSH_DAEMON_URL": "$DAEMON_URL"
      }
    }
  }
}
EOF
}
EOF
}

write_prompts() {
  prompt_file="$OUT_DIR/agent-instructions.txt"
  cat >"$prompt_file" <<'EOF'
For command execution, always use smartsh_run (or smartsh-agent/smartsh_agent if only custom tools are available).
Never use direct shell execution unless explicitly requested by the user.
Prefer summarized tool output and avoid dumping full terminal logs.
EOF
}

main() {
  require_cmd curl
  require_cmd go
  ensure_daemon
  write_cursor_tool
  write_cursor_mcp
  write_cursor_mcp_workspace
  write_claude_tool
  write_prompts

  info ""
  info "smartsh quick setup complete."
  info "Cursor tool file: $OUT_DIR/cursor-smartsh-tool.json"
  info "Cursor MCP server file: $OUT_DIR/cursor-smartsh-mcp.json"
  info "Cursor workspace mcp.json: $OUT_DIR/cursor-mcp.json"
  info "Claude tool file: $OUT_DIR/claude-smartsh-tool.json"
  info "Agent instruction snippet: $OUT_DIR/agent-instructions.txt"
  info ""
  info "Minimal next step:"
  info "1) Import the generated tool JSON file in Cursor/Claude."
  info "2) Paste agent-instructions.txt into system instructions."
}

main "$@"
