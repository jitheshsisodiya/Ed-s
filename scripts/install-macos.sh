#!/usr/bin/env bash
#
# Installs the NexusVPN client (nexusvpnctl) on macOS.
#
# Usage:
#   sudo ./install-macos.sh
#
set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
BIN_DIR="$PREFIX/bin"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: this installer is for macOS; use install-linux.sh on Linux" >&2
  exit 1
fi

if [[ $EUID -ne 0 ]]; then
  echo "error: this installer must run as root (it writes to $BIN_DIR)" >&2
  echo "try: sudo $0" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "error: Go is required to build the client (brew install go)" >&2
  exit 1
fi

echo "==> Building nexusvpnctl"
cd "$REPO_ROOT/client"
VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)"
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.Version=$VERSION" \
  -o /tmp/nexusvpnctl ./cmd/nexusvpnctl

echo "==> Installing to $BIN_DIR/nexusvpnctl"
mkdir -p "$BIN_DIR"
install -m 0755 /tmp/nexusvpnctl "$BIN_DIR/nexusvpnctl"
rm -f /tmp/nexusvpnctl

cat <<EOF

Installed $("$BIN_DIR/nexusvpnctl" version)

Next:
  sudo nexusvpnctl login -server https://your-control-plane
  sudo nexusvpnctl network join -code <INVITE CODE>
  sudo nexusvpnctl up

Notes for macOS:
  * Creating the tunnel needs root, so run 'up' with sudo. macOS assigns the
    interface a utunN name of its own choosing.
  * To run the tunnel at boot, wrap 'nexusvpnctl up' in a launchd job under
    /Library/LaunchDaemons (it must run as root for the same reason).
EOF
