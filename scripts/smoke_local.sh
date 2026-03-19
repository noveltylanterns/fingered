#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
PORT="${1:-7979}"
DOC_ROOT="${TMP}/public"
LOG_ROOT="${TMP}/logs"
CONF="${TMP}/fingered.conf"
BIN_DIR="${ROOT_DIR}/bin"
BIN="${BIN_DIR}/fingered-dev"
PID=""
RUN_CGI_SMOKE="${RUN_CGI_SMOKE:-auto}"
CGI_SMOKE=0

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" >/dev/null 2>&1; then
    kill "$PID" >/dev/null 2>&1 || true
    wait "$PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT

command -v go >/dev/null 2>&1 || { echo "go is required" >&2; exit 1; }

mkdir -p /tmp/go-build /tmp/go-tmp
mkdir -p "$BIN_DIR"
GOCACHE=/tmp/go-build GOTMPDIR=/tmp/go-tmp go build -o "$BIN" ./cmd/fingered

mkdir -p "$DOC_ROOT" "$LOG_ROOT"
printf 'HEADER\n' > "${DOC_ROOT}/.header.txt"
printf 'FOOTER\n' > "${DOC_ROOT}/.footer.txt"
printf 'INDEX\n' > "${DOC_ROOT}/index.txt"
printf 'HELLO\n' > "${DOC_ROOT}/hello.txt"

if [ "$RUN_CGI_SMOKE" = "yes" ]; then
  CGI_SMOKE=1
elif [ "$RUN_CGI_SMOKE" = "auto" ] && [ "$(id -u)" -eq 0 ]; then
  CGI_SMOKE=1
fi

if [ "$CGI_SMOKE" -eq 1 ]; then
cat > "${TMP}/dynamic.go" <<'EOF'
package main
import (
  "fmt"
  "os"
)
func main() {
  fmt.Printf("CGI OK\r\nargs=%d env=%d\r\n", len(os.Args), len(os.Environ()))
}
EOF
CGO_ENABLED=0 GOCACHE=/tmp/go-build GOTMPDIR=/tmp/go-tmp go build -o "${DOC_ROOT}/dynamic.cgi" "${TMP}/dynamic.go"
chmod 0755 "${DOC_ROOT}/dynamic.cgi"
fi

cat > "$CONF" <<EOF
bind_ip = 127.0.0.1
port = ${PORT}
doc_root = ${DOC_ROOT}
tpl_extend = no
tls_enable = no
tls_port = 8179
read_timeout_ms = 1000
write_timeout_ms = 1000
max_request_bytes = 256
cgi_timeout_ms = 1000
cgi_max_stdout_bytes = 262144
max_response_bytes = 262144
cgi_enable = $( [ "$CGI_SMOKE" -eq 1 ] && printf yes || printf no )
tpl_wrapper = yes
tpl_credits = yes
log_root = ${LOG_ROOT}
log_umask = 0007
log_format = rfc5424
log_errors = yes
log_requests = yes
proxy_protocol = no
EOF

"$BIN" -config "$CONF" &
PID="$!"
sleep 0.5

probe() {
  local payload="$1"
  if command -v nc >/dev/null 2>&1; then
    printf '%b' "$payload" | nc -w 2 127.0.0.1 "$PORT" | tr -d '\r'
    return
  fi
  timeout 3 bash -c 'exec 3<>"/dev/tcp/$1/$2"; printf "%b" "$3" >&3; cat <&3' _ 127.0.0.1 "$PORT" "$payload" | tr -d '\r'
}

probe_idle() {
  if command -v nc >/dev/null 2>&1; then
    timeout 2 nc 127.0.0.1 "$PORT" < /dev/null | tr -d '\r' || true
    return
  fi
  timeout 3 bash -c 'exec 3<>"/dev/tcp/$1/$2"; cat <&3' _ 127.0.0.1 "$PORT" | tr -d '\r' || true
}

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if ! printf '%s' "$haystack" | grep -Fq "$needle"; then
    echo "missing expected text: $needle" >&2
    printf '%s\n' "$haystack" >&2
    exit 1
  fi
}

assert_equals() {
  local got="$1"
  local want="$2"
  if [ "$got" != "$want" ]; then
    echo "unexpected output" >&2
    printf 'got : %q\n' "$got" >&2
    printf 'want: %q\n' "$want" >&2
    exit 1
  fi
}

idle="$(probe_idle)"
assert_equals "$idle" ""

index_out="$(probe '\r\n')"
assert_contains "$index_out" "HEADER"
assert_contains "$index_out" "INDEX"
assert_contains "$index_out" "FOOTER"
assert_contains "$index_out" "_____________________________"
assert_contains "$index_out" "finger://lanterns.io/fingered"

hello_out="$(probe 'hello\r\n')"
assert_contains "$hello_out" "HELLO"

dot_out="$(probe 'john.doe\r\n')"
assert_contains "$dot_out" "Error: No content configured for this request."

invalid_out="$(probe 'hello.txt\r\n')"
assert_equals "$invalid_out" "Error: Invalid Request"

if [ "$CGI_SMOKE" -eq 1 ]; then
  cgi_out="$(probe 'dynamic\r\n')"
  assert_contains "$cgi_out" "CGI OK"
  assert_contains "$cgi_out" "args=1 env=0"
else
  echo "skipping CGI smoke checks; run as root or set RUN_CGI_SMOKE=yes to force"
fi

for _ in $(seq 1 25); do
  probe 'bad/request\r\n' >/dev/null || true
done
post_out="$(probe 'hello\r\n')"
assert_contains "$post_out" "HELLO"

echo "local smoke passed"
