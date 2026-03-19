#!/usr/bin/env bash
set -euo pipefail

NOSYSD=0
ARCH="amd64"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --nosysd)
      NOSYSD=1
      shift
      ;;
    --arch)
      [ "$#" -ge 2 ] || { echo "missing value for --arch" >&2; exit 1; }
      ARCH="$2"
      shift 2
      ;;
    --arch=*)
      ARCH="${1#*=}"
      shift
      ;;
    *)
      echo "usage: $0 [--nosysd] [--arch 386|amd64|arm64|riscv64]" >&2
      exit 1
      ;;
  esac
done

case "$ARCH" in
  386|amd64|arm64|riscv64) ;;
  *)
    echo "unsupported arch: $ARCH" >&2
    exit 1
    ;;
esac

if [ "$(id -u)" -ne 0 ]; then
  echo "install_fingered.sh must be run as root" >&2
  exit 1
fi

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
BIN_SRC="${SCRIPT_DIR}/fingered-${ARCH}"
CLIENT_SRC="${SCRIPT_DIR}/finger.sh"
CONF_SRC="${ROOT_DIR}/contrib/fingered.conf.example"
UNIT_SRC="${ROOT_DIR}/contrib/fingered.service"
CONF_DST="/etc/fingered/fingered.conf"

if [ ! -x "$BIN_SRC" ]; then
  echo "expected built binary for ${ARCH} at $BIN_SRC" >&2
  exit 1
fi
if [ ! -f "$CLIENT_SRC" ]; then
  echo "expected client script at $CLIENT_SRC" >&2
  exit 1
fi

if ! command -v useradd >/dev/null 2>&1; then
  echo "useradd is required" >&2
  exit 1
fi

config_value() {
  local key="$1"
  local default_value="$2"
  local value
  value="$(sed -n -E "s/^[[:space:]]*${key}[[:space:]]*=[[:space:]]*(.+)[[:space:]]*$/\\1/p" "$CONF_DST" | tail -n 1)"
  if [ -z "$value" ]; then
    printf '%s\n' "$default_value"
  else
    printf '%s\n' "$value"
  fi
}

if ! getent passwd finger >/dev/null 2>&1; then
  useradd -m -c "finger document root" -s /sbin/nologin finger
fi

if ! getent passwd fingered >/dev/null 2>&1; then
  useradd -r -M -c "finger server daemon" -s /sbin/nologin fingered
fi

usermod -a -G finger fingered

install -d -m 0755 /usr/local/sbin
install -o root -g fingered -m 0750 "$BIN_SRC" /usr/local/sbin/fingered
install -d -m 0755 /usr/local/bin
install -o root -g root -m 0755 "$CLIENT_SRC" /usr/local/bin/finger

install -d -o root -g fingered -m 0750 /etc/fingered
install -d -o root -g fingered -m 0750 /etc/fingered/tls
if [ ! -f "$CONF_DST" ]; then
  install -m 0640 -o root -g fingered "$CONF_SRC" "$CONF_DST"
fi

LOG_ROOT="$(config_value log_root /home/finger/logs/fingered/)"
LOG_GROUP="$(config_value log_group finger)"
LOG_UMASK="$(config_value log_umask 0007)"
LOG_ERRORS="$(config_value log_errors yes)"
LOG_REQUESTS="$(config_value log_requests no)"

if ! getent group "$LOG_GROUP" >/dev/null 2>&1; then
  echo "configured log_group does not exist: $LOG_GROUP" >&2
  exit 1
fi

usermod -a -G "$LOG_GROUP" fingered

if ! [[ "$LOG_UMASK" =~ ^[0-7]{3,4}$ ]]; then
  echo "configured log_umask is invalid: $LOG_UMASK" >&2
  exit 1
fi

LOG_UMASK_NUM=$((8#$LOG_UMASK))
LOG_FILE_MODE="$(printf '%04o' $(( 8#0666 & ~LOG_UMASK_NUM )))"
LOG_DIR_MODE="$(printf '%04o' $(( 8#2777 & ~LOG_UMASK_NUM )))"

umask 027
install -d -o finger -g finger -m 0750 /home/finger
install -d -o finger -g finger -m 0750 /home/finger/app
install -d -o finger -g finger -m 0750 /home/finger/app/finger
install -d -o finger -g finger -m 0750 /home/finger/logs
install -d -o fingered -g "$LOG_GROUP" -m "$LOG_DIR_MODE" "$LOG_ROOT"
chown fingered:"$LOG_GROUP" "$LOG_ROOT"
chmod "$LOG_DIR_MODE" "$LOG_ROOT"

if [ "$LOG_ERRORS" = "yes" ] && [ ! -e "${LOG_ROOT}/error.log" ]; then
  : > "${LOG_ROOT}/error.log"
fi
if [ "$LOG_REQUESTS" = "yes" ] && [ ! -e "${LOG_ROOT}/access.log" ]; then
  : > "${LOG_ROOT}/access.log"
fi

for log_file in "${LOG_ROOT}/error.log" "${LOG_ROOT}/access.log"; do
  if [ -e "$log_file" ]; then
    chown fingered:"$LOG_GROUP" "$log_file"
    chmod "$LOG_FILE_MODE" "$log_file"
  fi
done

if [ "$NOSYSD" -eq 0 ]; then
  install -D -m 0644 "$UNIT_SRC" /etc/systemd/system/fingered.service
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
    systemctl enable fingered.service >/dev/null
  fi
fi

echo "fingered installed"
echo "binary: /usr/local/sbin/fingered"
echo "client: /usr/local/bin/finger"
echo "arch: ${ARCH}"
echo "config: ${CONF_DST}"
echo "doc_root: /home/finger/app/finger/"
echo "tls_root: /etc/fingered/tls/"
echo "log_root: ${LOG_ROOT}"
echo "log_group: ${LOG_GROUP}"
echo "log_umask: ${LOG_UMASK}"
if [ "$NOSYSD" -eq 0 ]; then
  echo "systemd unit: /etc/systemd/system/fingered.service"
else
  echo "systemd setup skipped (--nosysd)"
fi
