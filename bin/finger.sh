#!/bin/sh
#
#   finger.sh - network-only finger client (no local user enumeration)
#
#   https://github.com/HumphreyBoaGart/finger
#   finger://lanterns.io/fingered
#
#   Usage: finger.sh [host[:port] | @host[:port] | user@host[:port]]

set -eu

CR="$(printf '\r')"
TAB="$(printf '\t')"
NL='
'

usage() {
  echo "Usage: $0 [host[:port] | @host[:port] | user@host[:port]]" >&2
  exit 1
}

die() {
  echo "$0: $*" >&2
  exit 1
}

validate_query() {
  case "$1" in
    *"$CR"*|*"$NL"*|*"$TAB"* )
      die "query contains unsupported control characters"
      ;;
  esac
}

validate_host() {
  host="$1"
  [ -n "$host" ] || die "host must not be empty"
  case "$host" in
    -*)
      die "host must not begin with '-'"
      ;;
    *[!A-Za-z0-9._:-]*)
      die "host contains unsupported characters"
      ;;
  esac
}

validate_port() {
  port="$1"
  case "$port" in
    ''|*[!0-9]*)
      die "port must be decimal digits only"
      ;;
  esac
  [ "$port" -ge 1 ] 2>/dev/null || die "port must be between 1 and 65535"
  [ "$port" -le 65535 ] 2>/dev/null || die "port must be between 1 and 65535"
}

parse_hostport() {
  hostport="$1"
  case "$hostport" in
    \[*\]:*)
      host="${hostport%%]*}"
      host="${host#\[}"
      port="${hostport##*:}"
      ;;
    \[*\])
      host="${hostport#\[}"
      host="${host%\]}"
      port=79
      ;;
    *:*:*)
      host="$hostport"
      port=79
      ;;
    *:*)
      host="${hostport%%:*}"
      port="${hostport##*:}"
      ;;
    *)
      host="$hostport"
      port=79
      ;;
  esac
}

run_request() {
  host="$1"
  port="$2"
  query="$3"

  if command -v nc >/dev/null 2>&1; then
    printf '%s\r\n' "$query" | nc -w 5 "$host" "$port"
    return
  fi
  if command -v netcat >/dev/null 2>&1; then
    printf '%s\r\n' "$query" | netcat -w 5 "$host" "$port"
    return
  fi
  if command -v ncat >/dev/null 2>&1; then
    printf '%s\r\n' "$query" | ncat --recv-only "$host" "$port"
    return
  fi
  if command -v bash >/dev/null 2>&1; then
    exec bash -c '
      exec 3<>"/dev/tcp/$1/$2" || exit 1
      printf "%s\r\n" "$3" >&3
      cat <&3
    ' _ "$host" "$port" "$query"
  fi
  die "requires nc, netcat, or bash with /dev/tcp"
}

[ "$#" -eq 1 ] || usage

arg="$1"
case "$arg" in
  @*)
    hostport="${arg#@}"
    query=""
    ;;
  *@*)
    hostport="${arg##*@}"
    query="${arg%"@$hostport"}"
    ;;
  *)
    hostport="$arg"
    query=""
    ;;
esac

parse_hostport "$hostport"
validate_query "$query"
validate_host "$host"
validate_port "$port"
run_request "$host" "$port" "$query"
