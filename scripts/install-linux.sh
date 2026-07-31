#!/usr/bin/env bash
#
# Installs the NexusVPN client (nexusvpnctl) on Linux.
#
# Usage:
#   sudo ./install-linux.sh              # build from this checkout
#   sudo ./install-linux.sh --systemd    # also install a systemd unit
#
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BIN_DIR="$PREFIX/bin"
INSTALL_SYSTEMD=false
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for arg in "$@"; do
  case "$arg" in
    --systemd) INSTALL_SYSTEMD=true ;;
    -h|--help)
      sed -n '2,9p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "unknown option: $arg" >&2
      exit 1
      ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "error: this installer must run as root (it writes to $BIN_DIR)" >&2
  echo "try: sudo $0 $*" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required to build the client (https://go.dev/dl/)" >&2
  exit 1
fi

# iproute2 provides the `ip` command used to address and route the tunnel.
if ! command -v ip >/dev/null 2>&1; then
  echo "error: the 'ip' command (iproute2) is required" >&2
  exit 1
fi

echo "==> Building nexusvpnctl"
cd "$REPO_ROOT/client"
VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.Version=$VERSION" \
  -o /tmp/nexusvpnctl ./cmd/nexusvpnctl

echo "==> Installing to $BIN_DIR/nexusvpnctl"
install -m 0755 /tmp/nexusvpnctl "$BIN_DIR/nexusvpnctl"
rm -f /tmp/nexusvpnctl

if [[ "$INSTALL_SYSTEMD" == true ]]; then
  echo "==> Installing systemd unit"
  install -m 0644 "$REPO_ROOT/scripts/nexusvpn.service" /etc/systemd/system/nexusvpn.service
  systemctl daemon-reload
  cat <<'EOF'

The systemd unit is installed but not enabled. It runs `nexusvpnctl up`,
which needs an authenticated session first:

  sudo nexusvpnctl login -server https://your-control-plane
  sudo nexusvpnctl network join -code <INVITE CODE>

Then enable it:

  sudo systemctl enable --now nexusvpn

Note: the unit runs as root because creating a TUN interface requires
CAP_NET_ADMIN, and it reads the config from root's config directory.
EOF
fi

echo
echo "Installed $("$BIN_DIR/nexusvpnctl" version)"
echo
echo "Next:"
echo "  sudo nexusvpnctl login -server https://your-control-plane"
echo "  sudo nexusvpnctl network join -code <INVITE CODE>"
echo "  sudo nexusvpnctl up"
