#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd)"
USE_INSTALLED_BIN="${SMARTSH_USE_INSTALLED_BIN:-0}"
FORCE_GO_RUN="${SMARTSH_FORCE_GO_RUN:-0}"
LOCAL_BIN="$SCRIPT_DIR/bin/smartsh"

if [ "$FORCE_GO_RUN" != "1" ] && [ -x "$LOCAL_BIN" ]; then
  exec "$LOCAL_BIN" mcp
fi

if [ "$USE_INSTALLED_BIN" = "1" ] && command -v smartsh >/dev/null 2>&1; then
  exec smartsh mcp
fi

if [ "$FORCE_GO_RUN" = "1" ] && command -v go >/dev/null 2>&1; then
  cd "$SCRIPT_DIR"
  exec go run ./cmd/smartsh mcp
fi

if command -v smartsh >/dev/null 2>&1; then
  exec smartsh mcp
fi

echo "smartsh-mcp launcher failed: expected executable at $LOCAL_BIN (or set SMARTSH_USE_INSTALLED_BIN=1, or SMARTSH_FORCE_GO_RUN=1)" >&2
exit 1
