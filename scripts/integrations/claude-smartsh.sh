#!/usr/bin/env sh
set -eu

SMARTSH_DAEMON_URL="${SMARTSH_DAEMON_URL:-http://127.0.0.1:8787}"
SMARTSH_ASYNC="${SMARTSH_ASYNC:-1}"
SMARTSH_TIMEOUT_SEC="${SMARTSH_TIMEOUT_SEC:-180}"
SMARTSH_POLL_INTERVAL_SEC="${SMARTSH_POLL_INTERVAL_SEC:-0.4}"
SMARTSH_POLL_MAX_ATTEMPTS="${SMARTSH_POLL_MAX_ATTEMPTS:-300}"
SMARTSH_DAEMON_TOKEN="${SMARTSH_DAEMON_TOKEN:-}"

script_dir() {
  CDPATH= cd -- "$(dirname "$0")/../.." && pwd
}

start_daemon_if_needed() {
  if [ -n "$SMARTSH_DAEMON_TOKEN" ]; then
    if curl -sS --max-time 1 -H "X-Smartsh-Token: $SMARTSH_DAEMON_TOKEN" "$SMARTSH_DAEMON_URL/health" >/dev/null 2>&1; then
      return 0
    fi
  elif curl -sS --max-time 1 "$SMARTSH_DAEMON_URL/health" >/dev/null 2>&1; then
    return 0
  fi

  if command -v smartshd >/dev/null 2>&1; then
    nohup smartshd >/tmp/smartshd.log 2>&1 &
  else
    ROOT_DIR="$(script_dir)"
    nohup go run "$ROOT_DIR/cmd/smartshd" >/tmp/smartshd.log 2>&1 &
  fi

  attempts=0
  while [ "$attempts" -lt 20 ]; do
    if [ -n "$SMARTSH_DAEMON_TOKEN" ]; then
      if curl -sS --max-time 1 -H "X-Smartsh-Token: $SMARTSH_DAEMON_TOKEN" "$SMARTSH_DAEMON_URL/health" >/dev/null 2>&1; then
        return 0
      fi
    elif curl -sS --max-time 1 "$SMARTSH_DAEMON_URL/health" >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts + 1))
    sleep 0.2
  done

  echo "smartshd is not responding at $SMARTSH_DAEMON_URL" >&2
  return 1
}

build_payload() {
  python3 - "$@" <<'PY'
import json
import os
import sys

command = " ".join(sys.argv[1:]).strip()
payload = {
    "command": command,
    "cwd": os.getcwd(),
    "async": os.getenv("SMARTSH_ASYNC", "0") == "1",
}
timeout_sec = os.getenv("SMARTSH_TIMEOUT_SEC", "0").strip()
if timeout_sec and timeout_sec.isdigit() and int(timeout_sec) > 0:
    payload["timeout_sec"] = int(timeout_sec)
print(json.dumps(payload))
PY
}

post_run() {
  payload="$1"
  if [ -n "$SMARTSH_DAEMON_TOKEN" ]; then
    curl -sS -X POST "$SMARTSH_DAEMON_URL/run" -H "Content-Type: application/json" -H "X-Smartsh-Token: $SMARTSH_DAEMON_TOKEN" -d "$payload"
    return 0
  fi
  curl -sS -X POST "$SMARTSH_DAEMON_URL/run" -H "Content-Type: application/json" -d "$payload"
}

poll_job_until_done() {
  initial_response="$1"
  if [ "$SMARTSH_ASYNC" != "1" ]; then
    printf "%s" "$initial_response"
    return 0
  fi

  job_id="$(printf "%s" "$initial_response" | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("job_id",""))')"
  status="$(printf "%s" "$initial_response" | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("status",""))')"
  if [ -z "$job_id" ] || [ "$status" = "completed" ] || [ "$status" = "failed" ] || [ "$status" = "blocked" ]; then
    printf "%s" "$initial_response"
    return 0
  fi

  attempts=0
  while [ "$attempts" -lt "$SMARTSH_POLL_MAX_ATTEMPTS" ]; do
    if [ -n "$SMARTSH_DAEMON_TOKEN" ]; then
      polled="$(curl -sS -H "X-Smartsh-Token: $SMARTSH_DAEMON_TOKEN" "$SMARTSH_DAEMON_URL/jobs/$job_id")"
    else
      polled="$(curl -sS "$SMARTSH_DAEMON_URL/jobs/$job_id")"
    fi
    polled_status="$(printf "%s" "$polled" | python3 -c 'import json,sys; data=json.load(sys.stdin); print(data.get("status",""))')"
    if [ "$polled_status" = "completed" ] || [ "$polled_status" = "failed" ] || [ "$polled_status" = "blocked" ]; then
      printf "%s" "$polled"
      return 0
    fi
    attempts=$((attempts + 1))
    sleep "$SMARTSH_POLL_INTERVAL_SEC"
  done

  printf "%s" "$initial_response"
}

start_daemon_if_needed

if [ "$#" -gt 0 ]; then
  payload="$(build_payload "$@")"
  response="$(post_run "$payload")"
  poll_job_until_done "$response"
  exit 0
fi

if [ -t 0 ]; then
  echo "Usage: claude-smartsh.sh <command> OR pipe JSON/plain command to stdin" >&2
  exit 2
fi

stdin_payload="$(cat)"
trimmed_payload="$(printf "%s" "$stdin_payload" | tr -d '\n' | tr -d '\r' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
if [ -z "$trimmed_payload" ]; then
  echo '{"executed":false,"exit_code":1,"error":"empty input"}'
  exit 1
fi

if printf "%s" "$trimmed_payload" | grep -q '^{'; then
  response="$(post_run "$stdin_payload")"
  poll_job_until_done "$response"
  exit 0
fi

payload="$(build_payload "$trimmed_payload")"
response="$(post_run "$payload")"
poll_job_until_done "$response"
