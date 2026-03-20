#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: ./debug/fuzz_tpl_live.sh HOST [PORT] [plain|proxy]

Environment:
  ITERATIONS=40
  JOBS=8
  TIMEOUT=3
  CLIENT_IP=203.0.113.10
  PROXY_IP=127.0.0.1
EOF
}

HOST="${1:-}"
PORT="${2:-79}"
MODE="${3:-plain}"
ITERATIONS="${ITERATIONS:-40}"
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

INVALID_BODY="Error: Invalid Request"
TMP_DIR="$(mktemp -d /tmp/fingered-tpl.XXXXXX)"
trap 'rm -rf "$TMP_DIR"' EXIT

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
  frame_for "$payload" | nc -w "$TIMEOUT" "$HOST" "$PORT" 2>/dev/null | tr -d '\r'
}

record_failure() {
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
  local output

  checks=$((checks + 1))
  output="$(probe "$payload")"
  if [[ "$output" != *"$needle"* ]]; then
    record_failure "$label" "expected substring: $needle" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_exact_invalid() {
  local label="$1"
  local payload="$2"
  local output

  checks=$((checks + 1))
  output="$(probe "$payload")"
  if [ "$output" != "$INVALID_BODY" ]; then
    record_failure "$label" "expected exact invalid body" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_not_contains() {
  local label="$1"
  local payload="$2"
  local needle="$3"
  local output

  checks=$((checks + 1))
  output="$(probe "$payload")"
  if [[ "$output" == *"$needle"* ]]; then
    record_failure "$label" "unexpected substring: $needle" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

make_max_flags_payload() {
  local out="" i
  for i in $(seq 1 16); do
    out+="/f$(printf '%02d' "$i") "
  done
  out+="echo\r\n"
  printf '%b' "$out"
}

make_too_many_flags_payload() {
  local out="" i
  for i in $(seq 1 17); do
    out+="/f$(printf '%02d' "$i") "
  done
  out+="echo\r\n"
  printf '%b' "$out"
}

check_valid_spacing() {
  local output
  output="$(probe $'/PLAN   /mode=full     echo\r\n')"
  [[ "$output" == *'cgi-echo:/PLAN /mode=full echo'* ]]
}

check_duplicate_first_wins() {
  local output
  output="$(probe $'/PLAN /mode=full /PLAN /mode=short echo\r\n')"
  [[ "$output" == *'cgi-echo:/PLAN /mode=full echo'* ]] && [[ "$output" != *short* ]]
}

check_duplicate_variable_first_wins() {
  local output
  output="$(probe $'/mode=full /mode=short echo\r\n')"
  [[ "$output" == *'cgi-echo:/mode=full echo'* ]] && [[ "$output" != *short* ]]
}

check_flag_static_selection() {
  local output
  output="$(probe $'/PLAN index\r\n')"
  [[ "$output" == *'hello from fingered'* ]] && [[ "$output" != *'cgi-echo:'* ]]
}

check_w_compat() {
  local output
  output="$(probe $'/W   echo\r\n')"
  [[ "$output" == *'cgi-echo:/W echo'* ]]
}

run_stress_case() {
  local label="$1"
  local fn="$2"
  if ! "$fn" > /dev/null 2>&1; then
    printf '%s\n' "$label" > "$TMP_DIR/${label}.fail"
  fi
}

run_stress() {
  local i running=0 had_failures=0 path
  for ((i = 1; i <= ITERATIONS; i++)); do
    run_stress_case "spacing-$i" check_valid_spacing &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
    run_stress_case "dup-$i" check_duplicate_first_wins &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
    run_stress_case "dupvar-$i" check_duplicate_variable_first_wins &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
    run_stress_case "static-$i" check_flag_static_selection &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
    run_stress_case "wcompat-$i" check_w_compat &
    running=$((running + 1))
    if [ "$running" -ge "$JOBS" ]; then wait; running=0; fi
  done
  wait

  for path in "$TMP_DIR"/*.fail; do
    [ -e "$path" ] || continue
    had_failures=1
    failures=$((failures + 1))
    printf 'FAIL %s\n' "$(cat "$path")" >&2
  done

  checks=$((checks + ITERATIONS * 5))
  if [ "$had_failures" -eq 0 ]; then
    printf 'ok  tpl-stress (%s iterations, jobs=%s)\n' "$ITERATIONS" "$JOBS"
  fi
}

expect_contains "tpl-flag-only" $'/PLAN\r\n' "hello from fingered"
expect_contains "tpl-basic-canonical" $'/PLAN /mode=full echo\r\n' "cgi-echo:/PLAN /mode=full echo"
expect_contains "tpl-bare-and-variable" $'/PLAN /long_name=value_1 echo\r\n' "cgi-echo:/PLAN /long_name=value_1 echo"
expect_contains "tpl-duplicate-first-wins" $'/PLAN /mode=full /PLAN /mode=short echo\r\n' "cgi-echo:/PLAN /mode=full echo"
expect_not_contains "tpl-duplicate-short-dropped" $'/PLAN /mode=full /PLAN /mode=short echo\r\n' "short"
expect_contains "tpl-max-flags-16" "$(make_max_flags_payload)" "/f16 echo"
expect_exact_invalid "tpl-too-many-flags-17" "$(make_too_many_flags_payload)"
expect_exact_invalid "tpl-invalid-flag-char" $'/PL!N echo\r\n'
expect_exact_invalid "tpl-invalid-value-char" $'/mode=bad! echo\r\n'
expect_exact_invalid "tpl-slash-only" $'/\r\n'
expect_exact_invalid "tpl-flag-after-target" $'echo /PLAN\r\n'
expect_contains "tpl-static-target-unchanged" $'/PLAN index\r\n' "hello from fingered"
expect_not_contains "tpl-static-target-no-cgi" $'/PLAN index\r\n' "cgi-echo:"
expect_contains "tpl-w-compat" $'/W   echo\r\n' "cgi-echo:/W echo"

run_stress

printf '\nchecks=%s failures=%s host=%s port=%s mode=%s iterations=%s jobs=%s\n' "$checks" "$failures" "$HOST" "$PORT" "$MODE" "$ITERATIONS" "$JOBS"

if [ "$failures" -ne 0 ]; then
  exit 1
fi
