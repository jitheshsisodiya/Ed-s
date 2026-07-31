# NexusVPN

An open-source, self-hostable **virtual LAN**: create a private network,
invite devices with a code, and reach them by a stable virtual IP as if they
were on the same switch — encrypted end-to-end with WireGuard, connected
**directly** peer-to-peer whenever NAT allows, and **relayed automatically**
when it doesn't.

In the spirit of Radmin VPN / Hamachi / Tailscale, but yours to run.

<p align="center">
  <img src="desktop/docs/screenshot-login.png" alt="NexusVPN desktop app" width="640">
</p>

## How it works

1. A device registers with the control plane and is assigned a virtual IP
   from the network's CIDR.
2. It discovers its public endpoint and NAT class via STUN (two servers, so
   symmetric NAT is detectable).
3. Peers exchange ICE candidates through the control plane and attempt a
   simultaneous **UDP hole punch**.
4. If no WireGuard handshake completes in time, the client requests a
   **relay** and reroutes through it — the relay forwards ciphertext only and
   never holds the keys.
5. Handshake freshness is monitored continuously, so a Wi-Fi/cellular switch
   or NAT rebind reconnects on its own.

Full detail, with diagrams: [`docs/architecture.md`](docs/architecture.md).

## Components

| Path | What it is | Verified by |
|---|---|---|
| [`backend/`](backend/) | Control plane: REST + gRPC + WebSocket, Postgres, Redis | `go test ./... -race` (86 tests) |
| [`relay/`](relay/) | Authenticated UDP relay for NAT fallback | `go test ./... -race` (18 tests) |
| [`client/`](client/) | Tunnel engine + `nexusvpnctl` CLI | `go test ./... -race` (23 tunnel tests) |
| [`desktop/`](desktop/) | Wails app for Windows / macOS / Linux | `wails build`; launched and screenshotted |
| [`mobile/`](mobile/) | Flutter app for Android / iOS | `flutter analyze` clean, 17 tests |
| [`admin/`](admin/) | React + TypeScript + Tailwind admin panel | `npm run build` + `npm run lint` clean |
| [`sdk/`](sdk/) | Go and TypeScript API clients | tests pass in both |
| [`deploy/`](deploy/) | Docker Compose, Kubernetes, Prometheus, Grafana | manifests parse; nginx config validated |
| [`db/`](db/), [`api/`](api/), [`proto/`](proto/) | Schema, OpenAPI spec, gRPC contracts | — |

## Quick start

```bash
cp deploy/docker/.env.example deploy/docker/.env   # then edit the secrets
docker compose -f deploy/docker/docker-compose.yml up -d --build
```

That brings up Postgres, Redis, the control plane (REST `:8080`, gRPC `:9090`,
metrics `:9091`), a relay node, the admin panel (`:3000`), Prometheus and
Grafana. Migrations run automatically at startup.

Connect a device:

```bash
sudo ./scripts/install-linux.sh          # or install-macos.sh / install-windows.ps1
sudo nexusvpnctl login -server http://localhost:8080
sudo nexusvpnctl network create -name "Home Lab"
sudo nexusvpnctl up
```

On another machine, join with the invite code the previous command printed:

```bash
sudo nexusvpnctl network join -code <INVITE CODE>
sudo nexusvpnctl up
```

Both devices now hold an address in the network's CIDR and can reach each
other by it. `nexusvpnctl status` lists the network's devices; the running
`up` process reports whether each peer is **direct** or **relayed**.

## Security

- **End-to-end encryption.** WireGuard (Curve25519 + ChaCha20-Poly1305)
  terminates on the devices. Relays forward ciphertext and cannot decrypt it.
- **Private keys never leave the device.** Only public keys are registered.
  Local config is `0600` inside a `0700` directory.
- **Short-lived access tokens** (15 min) with rotating, revocable refresh
  tokens stored hashed; reusing a rotated token is rejected. A password reset
  revokes every existing session.
- **MFA** via TOTP, and RBAC (`owner` / `admin` / `member`) re-checked in the
  business layer on every operation — not just in middleware.
- **Relay sessions are scoped** to one device, one relay and one network by a
  signed token, with expiry enforced continuously. A relay never forwards for
  an address that hasn't authenticated.
- **Hardened deployment**: strict CSP on the admin panel, unprivileged
  containers (`runAsNonRoot`, read-only rootfs, all capabilities dropped),
  and a production start-up check that refuses default secrets.

Details in [`docs/architecture.md#security-model`](docs/architecture.md#security-model).
Reporting a vulnerability: [`SECURITY.md`](SECURITY.md).

## Development

Each component builds and tests independently:

```bash
cd backend && go test ./... -race
cd relay   && go test ./... -race
cd client  && go test ./... -race
cd admin   && npm ci && npm run build && npm run lint
cd mobile  && flutter pub get && flutter analyze && flutter test
cd desktop && wails build            # add -tags webkit2_41 on Linux
```

Or use the root [`Makefile`](Makefile) (`make test`, `make lint`, `make build`).

CI runs all of the above on every push, and builds the desktop app on
Windows, macOS and Linux runners, uploading each binary as an artifact.

## Known limitations

Stated plainly, rather than implied to work:

- **Mobile tunnel bring-up is unverified on a physical device.** The Dart
  layer is complete and tested, but the iOS Network Extension and Android
  `VpnService` need entitlements that require a paid Apple/Google developer
  account. See [`mobile/README.md`](mobile/README.md).
- **The Postgres repository layer has no live-database tests here.** Coverage
  sits at the use-case and transport layers against in-memory fakes; running
  the SQL against a real Postgres is what `docker compose up` does.
- **Single-node Postgres/Redis manifests** under `deploy/k8s/` are for demos.
  Use a managed service or an HA operator in production; see
  [`deploy/k8s/README.md`](deploy/k8s/README.md).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). The contracts in `api/openapi.yaml`,
`proto/`, and `db/migrations/` are the source of truth every component is
written against — change those first.

## License

MIT — see [`LICENSE`](LICENSE).
