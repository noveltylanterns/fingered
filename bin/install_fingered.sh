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
CONF_SRC="${ROOT_DIR}/contrib/fingered.conf.example"
UNIT_SRC="${ROOT_DIR}/contrib/fingered.service"

if [ ! -x "$BIN_SRC" ]; then
  echo "expected built binary for ${ARCH} at $BIN_SRC" >&2
  exit 1
fi

if ! command -v useradd >/dev/null 2>&1; then
  echo "useradd is required" >&2
  exit 1
fi

install -d -m 0755 /usr/local/sbin
install -m 0755 "$BIN_SRC" /usr/local/sbin/fingered

if ! getent passwd finger >/dev/null 2>&1; then
  useradd -m -c "finger document root" -s /sbin/nologin finger
fi

if ! getent passwd fingered >/dev/null 2>&1; then
  useradd -r -M -c "finger server daemon" -s /sbin/nologin fingered
fi

usermod -a -G finger fingered

install -d -o root -g root -m 0755 /etc/fingered
install -d -o root -g fingered -m 0750 /etc/fingered/tls
if [ ! -f /etc/fingered/fingered.conf ]; then
  install -m 0640 -o root -g finger "$CONF_SRC" /etc/fingered/fingered.conf
fi

umask 027
install -d -o finger -g finger -m 0750 /home/finger
install -d -o finger -g finger -m 0750 /home/finger/app
install -d -o finger -g finger -m 0750 /home/finger/app/public
install -d -o finger -g finger -m 0750 /home/finger/logs
install -d -o finger -g finger -m 2770 /home/finger/logs/fingered

if grep -Eq '^[[:space:]]*log_errors[[:space:]]*=[[:space:]]*yes[[:space:]]*$' /etc/fingered/fingered.conf; then
  install -m 0660 -o fingered -g finger /dev/null /home/finger/logs/fingered/error.log
fi

if grep -Eq '^[[:space:]]*log_requests[[:space:]]*=[[:space:]]*yes[[:space:]]*$' /etc/fingered/fingered.conf; then
  install -m 0660 -o fingered -g finger /dev/null /home/finger/logs/fingered/access.log
fi

if [ "$NOSYSD" -eq 0 ]; then
  install -D -m 0644 "$UNIT_SRC" /etc/systemd/system/fingered.service
  if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
    systemctl enable fingered.service >/dev/null
  fi
fi

echo "fingered installed"
echo "binary: /usr/local/sbin/fingered"
echo "arch: ${ARCH}"
echo "config: /etc/fingered/fingered.conf"
echo "doc_root: /home/finger/app/public/"
echo "tls_root: /etc/fingered/tls/"
echo "log_root: /home/finger/logs/fingered/"
if [ "$NOSYSD" -eq 0 ]; then
  echo "systemd unit: /etc/systemd/system/fingered.service"
else
  echo "systemd setup skipped (--nosysd)"
fi
