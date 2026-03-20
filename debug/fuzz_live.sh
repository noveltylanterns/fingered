#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF' >&2
usage: ./debug/fuzz_live.sh HOST [PORT] [plain|proxy]

Examples:
  ./debug/fuzz_live.sh 127.0.0.1 7979 proxy
  ./debug/fuzz_live.sh example.net 79 plain

Environment:
  TIMEOUT=3          per-probe timeout in seconds
  RANDOM_CASES=200   sequential generated invalid probes
  PARALLEL_CASES=64  concurrent generated invalid probes
  PARALLEL_JOBS=8    concurrent worker count
  BINARY_CASES=32    binary-garbage probes with CRLF terminator
  BINARY_BYTES=48    bytes of garbage before the terminator
  SLOWLORIS_CASES=4  incomplete-request timeout probes
  SLOWLORIS_HOLD_SECONDS=2  hold time before reading back
  OVERSIZE_BYTES=1024 request size for over-max probe
  SEED=12345         bash RANDOM seed
  CLIENT_IP=203.0.113.10   source IP for PROXY mode
  PROXY_IP=127.0.0.1       immediate peer IP for PROXY mode
  TRACE=0            set to 1 to print every response body
EOF
}

HOST="${1:-}"
PORT="${2:-79}"
MODE="${3:-plain}"

TIMEOUT="${TIMEOUT:-3}"
RANDOM_CASES="${RANDOM_CASES:-200}"
PARALLEL_CASES="${PARALLEL_CASES:-64}"
PARALLEL_JOBS="${PARALLEL_JOBS:-8}"
BINARY_CASES="${BINARY_CASES:-32}"
BINARY_BYTES="${BINARY_BYTES:-48}"
SLOWLORIS_CASES="${SLOWLORIS_CASES:-4}"
SLOWLORIS_HOLD_SECONDS="${SLOWLORIS_HOLD_SECONDS:-2}"
OVERSIZE_BYTES="${OVERSIZE_BYTES:-1024}"
SEED="${SEED:-12345}"
CLIENT_IP="${CLIENT_IP:-203.0.113.10}"
PROXY_IP="${PROXY_IP:-127.0.0.1}"
TRACE="${TRACE:-0}"

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

case "$PORT" in
  ''|*[!0-9]*)
    echo "port must be decimal digits" >&2
    exit 1
    ;;
esac

case "$TIMEOUT" in
  ''|*[!0-9]*)
    echo "TIMEOUT must be decimal digits" >&2
    exit 1
    ;;
esac

case "$RANDOM_CASES" in
  ''|*[!0-9]*)
    echo "RANDOM_CASES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$PARALLEL_CASES" in
  ''|*[!0-9]*)
    echo "PARALLEL_CASES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$PARALLEL_JOBS" in
  ''|*[!0-9]*)
    echo "PARALLEL_JOBS must be decimal digits" >&2
    exit 1
    ;;
esac

case "$BINARY_CASES" in
  ''|*[!0-9]*)
    echo "BINARY_CASES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$BINARY_BYTES" in
  ''|*[!0-9]*)
    echo "BINARY_BYTES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$SLOWLORIS_CASES" in
  ''|*[!0-9]*)
    echo "SLOWLORIS_CASES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$SLOWLORIS_HOLD_SECONDS" in
  ''|*[!0-9]*)
    echo "SLOWLORIS_HOLD_SECONDS must be decimal digits" >&2
    exit 1
    ;;
esac

case "$OVERSIZE_BYTES" in
  ''|*[!0-9]*)
    echo "OVERSIZE_BYTES must be decimal digits" >&2
    exit 1
    ;;
esac

case "$SEED" in
  ''|*[!0-9]*)
    echo "SEED must be decimal digits" >&2
    exit 1
    ;;
esac

if ! command -v nc >/dev/null 2>&1; then
  echo "nc is required" >&2
  exit 1
fi

RANDOM="$SEED"
INVALID_BODY='Error: Invalid Request'
TMP_DIR="$(mktemp -d /tmp/fingered-fuzz.XXXXXX)"

total_checks=0
failed_checks=0

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

rand_token() {
  local len="$1"
  local chars='abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._~-'
  local out=''
  local i idx
  for ((i = 0; i < len; i++)); do
    idx=$((RANDOM % ${#chars}))
    out+="${chars:$idx:1}"
  done
  printf '%s' "$out"
}

make_random_invalid() {
  local left right variant
  left="$(rand_token $((1 + RANDOM % 12)))"
  right="$(rand_token $((1 + RANDOM % 8)))"
  variant=$((RANDOM % 11))

  case "$variant" in
    0) printf '%s.txt\\r\\n' "$left" ;;
    1) printf '%s.cgi\\r\\n' "$left" ;;
    2) printf '%s/%s\\r\\n' "$left" "$right" ;;
    3) printf '%s\\\\%s\\r\\n' "$left" "$right" ;;
    4) printf '%s%%%s\\r\\n' "$left" "$right" ;;
    5) printf '%s@%s\\r\\n' "$left" "$right" ;;
    6) printf '.%s\\r\\n' "$left" ;;
    7) printf '%s.\\r\\n' "$left" ;;
    8) printf '%s %s\\r\\n' "$left" "$right" ;;
    9) printf '%s\\t%s\\r\\n' "$left" "$right" ;;
    10) printf '%s\\r\\n' "$(rand_token 65)" ;;
  esac
}

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

frame_file() {
  local path="$1"
  if [ "$MODE" = "proxy" ]; then
    printf 'PROXY TCP4 %s %s 40000 %s\r\n' "$CLIENT_IP" "$PROXY_IP" "$PORT"
  fi
  cat "$path"
}

probe_file() {
  local path="$1"
  local output status

  set +e
  output="$(
    frame_file "$path" | nc -w "$TIMEOUT" "$HOST" "$PORT" 2>&1 | tr -d '\r'
  )"
  status=$?
  set -e

  printf '%s' "$output"
  return "$status"
}

probe_raw_file() {
  local path="$1"
  local output status

  set +e
  output="$(
    cat "$path" | nc -w "$TIMEOUT" "$HOST" "$PORT" 2>&1 | tr -d '\r'
  )"
  status=$?
  set -e

  printf '%s' "$output"
  return "$status"
}

log_trace() {
  local label="$1"
  local output="$2"
  if [ "$TRACE" = "1" ]; then
    printf 'TRACE %s\n%s\n' "$label" "$output"
  fi
}

record_failure() {
  local label="$1"
  local expected="$2"
  local payload="$3"
  local status="$4"
  local output="$5"

  failed_checks=$((failed_checks + 1))
  printf 'FAIL %s\n' "$label" >&2
  printf '  expected: %s\n' "$expected" >&2
  printf '  status:   %s\n' "$status" >&2
  printf '  payload:  %q\n' "$payload" >&2
  printf '  output:\n%s\n' "$output" >&2
}

expect_invalid() {
  local label="$1"
  local payload="$2"
  local output status

  total_checks=$((total_checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e
  log_trace "$label" "$output"

  if [ "$status" -ne 0 ] || [ "$output" != "$INVALID_BODY" ]; then
    record_failure "$label" "exact invalid body" "$payload" "$status" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_valid() {
  local label="$1"
  local payload="$2"
  local output status

  total_checks=$((total_checks + 1))
  set +e
  output="$(probe "$payload")"
  status=$?
  set -e
  log_trace "$label" "$output"

  if [ "$status" -ne 0 ] || [ -z "$output" ] || [ "$output" = "$INVALID_BODY" ]; then
    record_failure "$label" "non-empty valid response" "$payload" "$status" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_invalid_file() {
  local label="$1"
  local path="$2"
  local payload_desc="${3:-<binary-payload>}"
  local output status

  total_checks=$((total_checks + 1))
  set +e
  output="$(probe_file "$path")"
  status=$?
  set -e
  log_trace "$label" "$output"

  if [ "$status" -ne 0 ] || [ "$output" != "$INVALID_BODY" ]; then
    record_failure "$label" "exact invalid body" "$payload_desc" "$status" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_invalid_raw_file() {
  local label="$1"
  local path="$2"
  local payload_desc="${3:-<raw-payload>}"
  local output status

  total_checks=$((total_checks + 1))
  set +e
  output="$(probe_raw_file "$path")"
  status=$?
  set -e
  log_trace "$label" "$output"

  if [ "$status" -ne 0 ] || [ "$output" != "$INVALID_BODY" ]; then
    record_failure "$label" "exact invalid body" "$payload_desc" "$status" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

expect_silent_close() {
  local label="$1"
  local prefix="$2"
  local output status timeout_seconds

  timeout_seconds=$((TIMEOUT + SLOWLORIS_HOLD_SECONDS + 2))
  total_checks=$((total_checks + 1))

  set +e
  output="$(
    HOST="$HOST" PORT="$PORT" MODE="$MODE" CLIENT_IP="$CLIENT_IP" PROXY_IP="$PROXY_IP" PREFIX="$prefix" SLOWLORIS_HOLD_SECONDS="$SLOWLORIS_HOLD_SECONDS" \
    timeout "$timeout_seconds" bash -c '
      exec 3<>"/dev/tcp/$HOST/$PORT" || exit 98
      if [ "$MODE" = "proxy" ]; then
        printf "PROXY TCP4 %s %s 40000 %s\r\n" "$CLIENT_IP" "$PROXY_IP" "$PORT" >&3
      fi
      if [ -n "$PREFIX" ]; then
        printf "%s" "$PREFIX" >&3
      fi
      sleep "$SLOWLORIS_HOLD_SECONDS"
      cat <&3
    ' 2>&1 | tr -d '\r'
  )"
  status=$?
  set -e
  log_trace "$label" "$output"

  if [ "$status" -ne 0 ] || [ -n "$output" ]; then
    record_failure "$label" "silent close" "$prefix" "$status" "$output"
    return
  fi
  printf 'ok  %s\n' "$label"
}

run_fixed_cases() {
  while IFS='|' read -r label kind payload; do
    [ -n "$label" ] || continue
    case "$kind" in
      valid) expect_valid "$label" "$payload" ;;
      invalid) expect_invalid "$label" "$payload" ;;
      *)
        echo "unknown case kind: $kind" >&2
        exit 1
        ;;
    esac
  done <<'EOF'
empty-request|valid|\r\n
legacy-W|valid|/W\r\n
index-target|valid|index\r\n
legacy-W-target|valid|/W hello\r\n
target-64-bytes|valid|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n
tpl-extend-flag|invalid|/PLAN\r\n
double-space-after-W|invalid|/W  hello\r\n
txt-extension|invalid|hello.txt\r\n
cgi-extension|invalid|hello.cgi\r\n
slash|invalid|foo/bar\r\n
backslash|invalid|foo\\bar\r\n
percent|invalid|foo%bar\r\n
at-sign|invalid|foo@bar\r\n
space-in-target|invalid|alice smith\r\n
tab-in-target|invalid|alice\tsmith\r\n
double-dot|invalid|..\r\n
leading-dot|invalid|.hidden\r\n
trailing-dot|invalid|hidden.\r\n
target-65-bytes|invalid|aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\n
nul-prefix|invalid|\000bad\r\n
lf-only-target|valid|hello\n
EOF
}

make_binary_invalid_file() {
  local path="$1"
  local i byte_code

  : > "$path"
  for ((i = 0; i < BINARY_BYTES; i++)); do
    case $((RANDOM % 10)) in
      0) byte_code='\\000' ;;
      1) byte_code='\\001' ;;
      2) byte_code='\\002' ;;
      3) byte_code='\\007' ;;
      4) byte_code='\\010' ;;
      5) byte_code='\\013' ;;
      6) byte_code='\\177' ;;
      7) byte_code='\\200' ;;
      8) byte_code='\\377' ;;
      9) byte_code='\\033' ;;
    esac
    printf '%b' "$byte_code" >> "$path"
  done
  printf '\r\n' >> "$path"
}

make_oversized_request_file() {
  local path="$1"
  local i

  : > "$path"
  for ((i = 0; i < OVERSIZE_BYTES; i++)); do
    printf 'a' >> "$path"
  done
  printf '\r\n' >> "$path"
}

run_random_invalids() {
  local i payload output status start_failures
  start_failures="$failed_checks"
  for ((i = 1; i <= RANDOM_CASES; i++)); do
    payload="$(make_random_invalid)"
    total_checks=$((total_checks + 1))
    set +e
    output="$(probe "$payload")"
    status=$?
    set -e
    log_trace "random-invalid-$i" "$output"

    if [ "$status" -ne 0 ] || [ "$output" != "$INVALID_BODY" ]; then
      record_failure "random-invalid-$i" "exact invalid body" "$payload" "$status" "$output"
    fi
  done
  if [ "$failed_checks" -eq "$start_failures" ]; then
    printf 'ok  random-invalid-batch (%s cases)\n' "$RANDOM_CASES"
  fi
}

run_binary_invalids() {
  local i path
  for ((i = 1; i <= BINARY_CASES; i++)); do
    RANDOM=$((SEED + 5000 + i))
    path="$TMP_DIR/binary-$i.bin"
    make_binary_invalid_file "$path"
    expect_invalid_file "binary-invalid-$i" "$path" "<binary-invalid-$i>"
  done
}

run_oversized_request() {
  local path="$TMP_DIR/oversized-request.bin"
  make_oversized_request_file "$path"
  expect_invalid_file "oversized-request" "$path" "<oversized-${OVERSIZE_BYTES}-byte-request>"
}

run_proxy_header_invalids() {
  local path

  if [ "$MODE" != "proxy" ]; then
    return
  fi

  path="$TMP_DIR/proxy-lf-only.bin"
  printf 'PROXY TCP4 %s %s 40000 %s\n' "$CLIENT_IP" "$PROXY_IP" "$PORT" > "$path"
  expect_invalid_raw_file "proxy-header-lf-only" "$path" "<proxy-header-lf-only>"

  path="$TMP_DIR/proxy-family-mismatch.bin"
  printf 'PROXY TCP4 2001:db8::10 %s 40000 %s\r\n' "$PROXY_IP" "$PORT" > "$path"
  expect_invalid_raw_file "proxy-header-family-mismatch" "$path" "<proxy-header-family-mismatch>"

  path="$TMP_DIR/proxy-port-zero.bin"
  printf 'PROXY TCP4 %s %s 0 %s\r\n' "$CLIENT_IP" "$PROXY_IP" "$PORT" > "$path"
  expect_invalid_raw_file "proxy-header-port-zero" "$path" "<proxy-header-port-zero>"
}

run_parallel_invalids() {
  local i batch path payload_dir result_dir had_failures

  if [ "$PARALLEL_CASES" -eq 0 ] || [ "$PARALLEL_JOBS" -eq 0 ]; then
    return
  fi

  payload_dir="$TMP_DIR/parallel-payloads"
  result_dir="$TMP_DIR/parallel-results"
  mkdir -p "$payload_dir" "$result_dir"

  total_checks=$((total_checks + PARALLEL_CASES))
  batch=0

  for ((i = 1; i <= PARALLEL_CASES; i++)); do
    (
      RANDOM=$((SEED + 10000 + i))
      path="$payload_dir/$i.txt"
      printf '%b' "$(make_random_invalid)" > "$path"
      set +e
      output="$(probe_file "$path")"
      status=$?
      set -e
      if [ "$status" -ne 0 ] || [ "$output" != "$INVALID_BODY" ]; then
        {
          printf 'FAIL parallel-invalid-%s\n' "$i"
          printf '  expected: exact invalid body\n'
          printf '  status:   %s\n' "$status"
          printf '  payload:  %q\n' "$(cat "$path")"
          printf '  output:\n%s\n' "$output"
        } > "$result_dir/$i.fail"
      fi
    ) &
    batch=$((batch + 1))
    if [ "$batch" -ge "$PARALLEL_JOBS" ]; then
      set +e
      wait
      set -e
      batch=0
    fi
  done

  set +e
  wait
  set -e

  had_failures=0
  for path in "$result_dir"/*.fail; do
    [ -e "$path" ] || continue
    had_failures=1
    failed_checks=$((failed_checks + 1))
    cat "$path" >&2
  done

  if [ "$had_failures" -eq 0 ]; then
    printf 'ok  parallel-invalid-batch (%s cases, jobs=%s)\n' "$PARALLEL_CASES" "$PARALLEL_JOBS"
  fi
}

run_slowloris_cases() {
  local i

  if [ "$SLOWLORIS_CASES" -eq 0 ]; then
    return
  fi

  expect_silent_close "idle-timeout" ""
  for ((i = 1; i <= SLOWLORIS_CASES; i++)); do
    expect_silent_close "slowloris-partial-$i" "hel"
  done
}

run_fixed_cases
run_oversized_request
run_proxy_header_invalids
run_random_invalids
run_parallel_invalids
run_binary_invalids
run_slowloris_cases
expect_valid "post-batch-empty-request" '\r\n'

printf '\nchecks=%s failures=%s host=%s port=%s mode=%s seed=%s\n' \
  "$total_checks" "$failed_checks" "$HOST" "$PORT" "$MODE" "$SEED"

if [ "$failed_checks" -ne 0 ]; then
  exit 1
fi
