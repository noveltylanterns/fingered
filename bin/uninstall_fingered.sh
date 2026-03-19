#!/usr/bin/env bash
set -euo pipefail

if [ "$#" -ne 0 ]; then
  echo "usage: $0" >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ]; then
  echo "uninstall_fingered.sh must be run as root" >&2
  exit 1
fi

BIN_DST="/usr/local/sbin/fingered"
CLIENT_DST="/usr/local/bin/finger"
CONF_DST="/etc/fingered/fingered.conf"
ETC_ROOT="/etc/fingered"
UNIT_DST="/etc/systemd/system/fingered.service"
UNIT_OVERRIDE_DIR="/etc/systemd/system/fingered.service.d"
UNIT_WANTS_LINK="/etc/systemd/system/multi-user.target.wants/fingered.service"
HOME_ROOT="/home/finger"

config_value() {
  local key="$1"
  local default_value="$2"
  local value

  if [ ! -f "$CONF_DST" ]; then
    printf '%s\n' "$default_value"
    return
  fi

  value="$(sed -n -E "s/^[[:space:]]*${key}[[:space:]]*=[[:space:]]*(.+)[[:space:]]*$/\\1/p" "$CONF_DST" | tail -n 1)"
  if [ -z "$value" ]; then
    printf '%s\n' "$default_value"
  else
    printf '%s\n' "$value"
  fi
}

remove_file() {
  local path="$1"
  if [ -e "$path" ] || [ -L "$path" ]; then
    rm -f -- "$path"
  fi
}

remove_tree() {
  local path="$1"
  if [ -d "$path" ] || [ -L "$path" ]; then
    rm -rf --one-file-system -- "$path"
  fi
}

safe_remove_managed_tree() {
  local path="$1"
  [ -n "$path" ] || return 0

  case "$path" in
    /home/finger|/home/finger/*|/etc/fingered|/etc/fingered/*)
      remove_tree "$path"
      ;;
    *)
      if [ -e "$path" ] || [ -L "$path" ]; then
        echo "skipping external configured path: $path" >&2
      fi
      ;;
  esac
}

DOC_ROOT="$(config_value doc_root /home/finger/app/finger/)"
TLS_DOC_ROOT="$(config_value tls_doc_root "")"
LOG_ROOT="$(config_value log_root /home/finger/logs/fingered/)"

if command -v systemctl >/dev/null 2>&1; then
  systemctl disable --now fingered.service >/dev/null 2>&1 || true
  systemctl reset-failed fingered.service >/dev/null 2>&1 || true
fi

if command -v pkill >/dev/null 2>&1; then
  pkill -u fingered >/dev/null 2>&1 || true
fi

remove_file "$BIN_DST"
remove_file "$CLIENT_DST"
remove_file "$UNIT_DST"
remove_file "$UNIT_WANTS_LINK"
remove_tree "$UNIT_OVERRIDE_DIR"

safe_remove_managed_tree "$LOG_ROOT"
safe_remove_managed_tree "$DOC_ROOT"
if [ -n "$TLS_DOC_ROOT" ] && [ "$TLS_DOC_ROOT" != "$DOC_ROOT" ]; then
  safe_remove_managed_tree "$TLS_DOC_ROOT"
fi
safe_remove_managed_tree "$ETC_ROOT"

if getent passwd fingered >/dev/null 2>&1; then
  userdel fingered
fi

if getent passwd finger >/dev/null 2>&1; then
  userdel -r finger
fi

if getent group fingered >/dev/null 2>&1; then
  groupdel fingered
fi

if getent group finger >/dev/null 2>&1; then
  groupdel finger
fi

safe_remove_managed_tree "$HOME_ROOT"

if command -v systemctl >/dev/null 2>&1; then
  systemctl daemon-reload >/dev/null 2>&1 || true
fi

echo "fingered uninstalled"
echo "removed: ${BIN_DST}"
echo "removed: ${CLIENT_DST}"
echo "removed: ${UNIT_DST}"
echo "removed: ${ETC_ROOT}"
echo "removed: ${HOME_ROOT}"
