#!/usr/bin/env sh
set -eu

if [ "$#" -gt 0 ]; then
  if command -v smartsh >/dev/null 2>&1; then
    exec smartsh --agent "$@"
  fi
  SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
  exec go run "$SCRIPT_DIR/cmd/smartsh" --agent "$@"
fi

if [ -t 0 ]; then
  echo "Usage: cursor-smartsh.sh <instruction> OR pipe JSON/plain instruction to stdin" >&2
  exit 2
fi

if command -v smartsh >/dev/null 2>&1; then
  exec smartsh --agent
fi
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
exec go run "$SCRIPT_DIR/cmd/smartsh" --agent
