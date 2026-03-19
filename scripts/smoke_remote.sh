#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-}"
PORT="${2:-79}"
SELECTOR="${3:-}"

if [ -z "$HOST" ]; then
  echo "usage: $0 HOST [PORT] [SELECTOR]" >&2
  exit 1
fi

probe() {
  local payload="$1"
  if command -v nc >/dev/null 2>&1; then
    printf '%b' "$payload" | nc -w 2 "$HOST" "$PORT" | tr -d '\r'
    return
  fi
  timeout 3 bash -c 'exec 3<>"/dev/tcp/$1/$2"; printf "%b" "$3" >&3; cat <&3' _ "$HOST" "$PORT" "$payload" | tr -d '\r'
}

if command -v nc >/dev/null 2>&1; then
  idle="$(timeout 2 nc "$HOST" "$PORT" < /dev/null | tr -d '\r' || true)"
else
  idle="$(timeout 3 bash -c 'exec 3<>"/dev/tcp/$1/$2"; cat <&3' _ "$HOST" "$PORT" | tr -d '\r' || true)"
fi
if [ -n "$idle" ]; then
  echo "unexpected banner output" >&2
  exit 1
fi

invalid_txt="$(probe 'hello.txt\r\n' || true)"
if [ "$invalid_txt" != "Error: Invalid Request" ]; then
  echo "invalid .txt probe did not return the expected generic error" >&2
  exit 1
fi

invalid_at="$(probe 'root@example.com\r\n' || true)"
if [ "$invalid_at" != "Error: Invalid Request" ]; then
  echo "invalid forwarding probe did not return the expected generic error" >&2
  exit 1
fi

if [ -n "$SELECTOR" ]; then
  valid_out="$(probe "${SELECTOR}\r\n" || true)"
  printf '%s\n' "$valid_out"
else
  root_out="$(probe '\r\n' || true)"
  printf '%s\n' "$root_out"
fi

echo "remote smoke probes completed"
