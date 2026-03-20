#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: ./debug/fuzz_cgi_live.sh HOST [PORT] [plain|proxy]

Environment:
  TIMEOUT=3
  CLIENT_IP=203.0.113.10
  PROXY_IP=127.0.0.1
EOF
}

HOST="${1:-}"
PORT="${2:-79}"
MODE="${3:-plain}"
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

HEADER="sample header template"
FOOTER="sample footer template"
CREDITS="finger://lanterns.io/fingered"
NOCONTENT="Error: No content configured for this request."

checks=0
failures=0

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
  local output status

  set +e
  output="$(
    frame_for "$payload" | nc -w "$TIMEOUT" "$HOST" "$PORT" 2>&1 | tr -d '\r'
  )"
  status=$?
  set -e

  printf '%s' "$output"
  return "$status"
}

fail_case() {
  local label="$1"
  local why="$2"
  local body="$3"
  failures=$((failures + 1))
  printf 'FAIL %s\n' "$label" >&2
  printf '  why: %s\n' "$why" >&2
  printf '  body:\n%s\n' "$body" >&2
}

expect_contains() {
  local label="$1"
  local payload="$2"
  local needle="$3"
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" != *"$needle"* ]]; then
    fail_case "$label" "expected substring: $needle" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_not_contains() {
  local label="$1"
  local payload="$2"
  local needle="$3"
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" == *"$needle"* ]]; then
    fail_case "$label" "unexpected substring: $needle" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_wrapped_no_content() {
  local label="$1"
  local payload="$2"
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" != *"$HEADER"* ]] || [[ "$output" != *"$FOOTER"* ]] || [[ "$output" != *"$NOCONTENT"* ]] || [[ "$output" != *"$CREDITS"* ]]; then
    fail_case "$label" "expected wrapped no-content response" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_any_no_content() {
  local label="$1"
  local payload="$2"
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" != *"$NOCONTENT"* ]]; then
    fail_case "$label" "expected no-content fallback" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_exact_index_precedence() {
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe $'\r\n')"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" == *"cgi-index-should-not-win"* ]] || [[ "$output" != *"hello from fingered"* ]]; then
    fail_case "static-index-precedence" "expected index.txt to beat index.cgi" "$output"
    return
  fi
  printf 'ok  static-index-precedence\n'
}

expect_canonical_echo() {
  expect_contains "cgi-echo-basic" $'echo\r\n' "cgi-echo:echo"
  expect_contains "cgi-echo-legacy-W" $'/W echo\r\n' "cgi-echo:/W echo"
}

expect_sanitized_control() {
  local output status

  checks=$((checks + 1))
  set +e
  output="$(probe $'control\r\n')"
  status=$?
  set -e

  if [ "$status" -ne 0 ] || [[ "$output" != *"cgi-control:?	?"* ]]; then
    fail_case "cgi-control-sanitized" "expected control bytes replaced with ?" "$output"
    return
  fi
  printf 'ok  cgi-control-sanitized\n'
}

expect_exact_index_precedence
expect_canonical_echo
expect_sanitized_control
expect_wrapped_no_content "cgi-timeout-fallback" $'slow\r\n'
expect_any_no_content "cgi-stdout-cap-fallback" $'big\r\n'
expect_wrapped_no_content "cgi-nul-fallback" $'nul\r\n'
expect_wrapped_no_content "cgi-fail-fallback" $'fail\r\n'
expect_not_contains "cgi-stderr-not-leaked" $'fail\r\n' "cgi probe stderr marker"

printf '\nchecks=%s failures=%s host=%s port=%s mode=%s\n' "$checks" "$failures" "$HOST" "$PORT" "$MODE"

if [ "$failures" -ne 0 ]; then
  exit 1
fi
