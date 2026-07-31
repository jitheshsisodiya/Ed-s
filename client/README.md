# NexusVPN Client

`nexusvpnctl` is the cross-platform client agent. It authenticates against a
control plane, joins virtual networks, and brings up a userspace WireGuard
tunnel that reaches peers **directly** when NAT allows, and through a
**relay** when it doesn't.

Builds for Windows 11, Windows Server, macOS and Linux from one codebase
(userspace WireGuard via `wireguard-go`, so no kernel module is required).

## Install

```bash
go build -o nexusvpnctl ./cmd/nexusvpnctl
```

Cross-compiling is plain Go:

```bash
GOOS=windows GOARCH=amd64 go build -o nexusvpnctl.exe ./cmd/nexusvpnctl
GOOS=darwin  GOARCH=arm64 go build -o nexusvpnctl     ./cmd/nexusvpnctl
```

## Usage

```bash
nexusvpnctl login -server https://api.nexusvpn.example.com
nexusvpnctl network join -code <INVITE CODE>
sudo nexusvpnctl up
```

`up` runs in the foreground and holds the tunnel for its lifetime; Ctrl-C
disconnects. `status` shows the network's devices and works without a
running tunnel.

| Command | Purpose |
|---|---|
| `login` / `logout` | Authenticate (prompts for password; supports MFA) |
| `network list` / `create` / `join` / `leave` | Manage networks |
| `up` | Bring up the tunnel (**requires root/Administrator**) |
| `down` | Explains how to stop the foreground tunnel |
| `status` | Show devices on a network |
| `device rotate-key` | Generate a new device keypair |

## Required privileges

Creating a TUN interface is privileged on every platform:

- **Linux** — run with `sudo` (or grant `CAP_NET_ADMIN`). Uses `iproute2`.
- **macOS** — run with `sudo`. The OS assigns a `utunN` name.
- **Windows** — run as Administrator, and install the
  [Wintun](https://www.wintun.net/) driver (`wintun.dll` beside the binary
  or in `System32`). Addressing is applied via PowerShell.

## How connectivity is established

Implemented in `internal/tunnel`, following `docs/architecture.md`:

1. **Register** the device over gRPC; the control plane assigns a virtual IP.
2. **Discover** the public endpoint and NAT type via STUN (two servers, so
   symmetric NAT is detectable).
3. **Heartbeat** endpoint and counters; the server sets the cadence.
4. **Stream peer updates** and keep the WireGuard peer table live.
5. Per peer: **exchange ICE candidates**, then **hole punch** — probes are
   fired at every candidate until a fresh WireGuard handshake appears.
6. If no handshake lands in time, **request a relay** and repoint the peer's
   endpoint at a local proxy that speaks the relay protocol.
7. **Monitor** handshake freshness and renegotiate stale paths, which is
   what makes reconnection automatic after a network change.

Peers are installed with `AllowedIPs` of just their own `/32`, so this is a
mesh of point-to-point routes, not a default-route VPN.

## Security

- The **private key never leaves the device**. Only the public key is sent
  to the control plane.
- Config (tokens + private key) is written atomically to `0600` inside a
  `0700` directory under the per-OS config dir (`os.UserConfigDir()`).
- Relay traffic is **end-to-end encrypted**: the relay forwards ciphertext
  and never holds the keys (WireGuard's handshake terminates on the peers).
- The relay proxy binds **loopback only**, so nothing off-host can inject
  packets into the tunnel.
- gRPC uses TLS by default. `-insecure-skip-verify` exists for self-signed
  test deployments, prints a warning, and should never be used in production.
- `device rotate-key` invalidates the old key network-wide; the next `up`
  re-registers.

## Package layout

```
cmd/nexusvpnctl/   CLI
internal/tunnel/   connect state machine (direct → relay fallback, monitoring)
internal/wireguard/ userspace WireGuard device + per-OS addressing/routing
internal/stun/     STUN client and NAT classification
internal/holepunch/ simultaneous UDP hole punching
internal/relayproxy/ loopback bridge to a relay node
internal/coordination/ gRPC client for CoordinationService
internal/apiclient/ REST client (auth, networks, devices)
internal/config/   config + keystore (0600 at rest)
```

## Tests

```bash
go test ./... -race
```

`internal/tunnel` tests drive the full state machine against fakes — direct
connection, relay fallback (verified down to the real BIND frame arriving at
a stand-in relay socket), NAT-rebind endpoint following, stream reconnection
and clean shutdown — with no TUN device or root required.

## Regenerating gRPC stubs

```bash
cd proto && buf generate      # regenerates backend/gen
```

The client's copy in `internal/coordination/gen` is generated from the same
`proto/coordination/v1/coordination.proto`.
