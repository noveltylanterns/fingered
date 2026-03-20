#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: ./debug/stress_cgi_live.sh HOST [PORT] [plain|proxy]

Environment:
  ITERATIONS=25
  JOBS=8
  TIMEOUT=3
  CLIENT_IP=203.0.113.10
  PROXY_IP=127.0.0.1
EOF
}

HOST="${1:-}"
PORT="${2:-79}"
MODE="${3:-plain}"
ITERATIONS="${ITERATIONS:-25}"
JOBS="${JOBS:-8}"
TIMEOUT="${TIMEOUT:-3}"
CLIENT_IP="${CLIENT_IP:-203.0.113.10}"
PROXY_IP="${PROXY_IP:-127.0.0.1}"

if [ -z "$HOST" ]; then
  usage
  exit 1
fi

case "$MODE" in
  plain|proxy) ;;
  *)
    usage
    exit 1
    ;;
esac

TMP_DIR="$(mktemp -d /tmp/fingered-cgi-stress.XXXXXX)"
trap 'rm -rf "$TMP_DIR"' EXIT

HEADER="sample header template"
FOOTER="sample footer template"
CREDITS="finger://lanterns.io/fingered"
NOCONTENT="Error: No content configured for this request."
FAILURES=0
CHECKS=0

frame_for() {
  local payload="$1"
  if [ "$MODE" = "proxy" ]; then
    printf 'PROXY TCP4 %s %s 40000 %s\r\n%b' "$CLIENT_IP" "$PROXY_IP" "$PORT" "$payload"
    return
  fi
  printf '%b' "$payload"
}

probe() {
  local payload="$1"
  frame_for "$payload" | nc -w "$TIMEOUT" "$HOST" "$PORT" 2>/dev/null | tr -d '\r'
}

check_echo() {
  local output
  output="$(probe $'echo\r\n')"
  [[ "$output" == *"cgi-echo:echo"* ]] || return 1
  [[ "$output" == *"$HEADER"* ]] || return 1
  [[ "$output" == *"$FOOTER"* ]] || return 1
  [[ "$output" == *"$CREDITS"* ]] || return 1
}

check_slow() {
  local output
  output="$(probe $'slow\r\n')"
  [[ "$output" == *"$NOCONTENT"* ]] || return 1
  [[ "$output" == *"$HEADER"* ]] || return 1
  [[ "$output" == *"$FOOTER"* ]] || return 1
  [[ "$output" == *"$CREDITS"* ]] || return 1
}

check_big() {
  local output
  output="$(probe $'big\r\n')"
  [[ "$output" == *"$NOCONTENT"* ]] || return 1
}

check_control() {
  local output
  output="$(probe $'control\r\n')"
  [[ "$output" == *$'cgi-control:?\t?'* ]] || return 1
}

run_case() {
  local label="$1"
  local fn="$2"
  local out="$TMP_DIR/${label}.out"
  if "$fn" >"$out" 2>&1; then
    :
  else
    printf '%s\n' "$label" >"$TMP_DIR/${label}.fail"
    cat "$out" >"$TMP_DIR/${label}.body"
  fi
}

batch() {
  local prefix="$1"
  local i running=0
  for ((i = 1; i <= ITERATIONS; i++)); do
    run_case "${prefix}-echo-$i" check_echo &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then
      wait
      running=0
    fi
    run_case "${prefix}-slow-$i" check_slow &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then
      wait
      running=0
    fi
    run_case "${prefix}-big-$i" check_big &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then
      wait
      running=0
    fi
    run_case "${prefix}-control-$i" check_control &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then
      wait
      running=0
    fi
  done
  wait
}

batch "$MODE"

for fail in "$TMP_DIR"/*.fail; do
  [ -e "$fail" ] || continue
  FAILURES=$((FAILURES + 1))
  label="$(cat "$fail")"
  printf 'FAIL %s\n' "$label" >&2
done

CHECKS=$((ITERATIONS * 4))
if [ "$FAILURES" -eq 0 ]; then
  printf 'checks=%s failures=0 host=%s port=%s mode=%s iterations=%s jobs=%s\n' "$CHECKS" "$HOST" "$PORT" "$MODE" "$ITERATIONS" "$JOBS"
else
  printf 'checks=%s failures=%s host=%s port=%s mode=%s iterations=%s jobs=%s\n' "$CHECKS" "$FAILURES" "$HOST" "$PORT" "$MODE" "$ITERATIONS" "$JOBS" >&2
  exit 1
fi
