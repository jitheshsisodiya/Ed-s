# NexusVPN

NexusVPN is an open-source virtual LAN / mesh VPN platform: create private
networks, invite devices by code, and reach them as if they were on the same
LAN — encrypted end-to-end with WireGuard, direct peer-to-peer whenever NAT
allows it, relayed automatically when it doesn't.

Inspired by Radmin VPN / Hamachi / Tailscale, built to be self-hosted.

## Repository layout

```
backend/    Go control plane: REST + gRPC + WebSocket API, Postgres, Redis
relay/      Go relay (TURN-like) server for NAT fallback, horizontally scalable
client/     Go client core: WireGuard integration, STUN/ICE, hole punching
desktop/    Wails (Go + React/TS) desktop app for Windows/Windows Server/Linux/macOS
mobile/     Flutter app for Android/iOS
admin/      React + TypeScript + Tailwind admin panel
db/         SQL migrations
proto/      gRPC service definitions (coordination service)
api/        OpenAPI 3.0 specification
sdk/        Generated/hand-written Go and TypeScript SDKs
deploy/     docker-compose, Kubernetes manifests, Prometheus/Grafana
docs/       Architecture, ER, sequence and deployment diagrams
.github/    CI/CD workflows
```

See [`docs/architecture.md`](docs/architecture.md) for the full architecture,
connectivity strategy, ER diagram, sequence diagrams and deployment diagram.

## Quick start (local, Docker Compose)

```bash
cp deploy/docker/.env.example deploy/docker/.env
docker compose -f deploy/docker/docker-compose.yml up -d --build
```

This brings up Postgres, Redis, the backend API (REST :8080, gRPC :9090,
metrics :9091), a relay node, and the admin panel (:3000). Migrations run
automatically on backend startup.

Register your first account against the API, create a network, and connect
a device using the client agent:

```bash
go run ./client/cmd/nexusvpnctl login --server https://localhost:8443
go run ./client/cmd/nexusvpnctl network join --code <INVITE_CODE>
go run ./client/cmd/nexusvpnctl up
```

## Development

- Backend: `cd backend && go run ./cmd/api` (needs `DATABASE_URL`, `REDIS_URL`)
- Admin panel: `cd admin && npm install && npm run dev`
- Client: `cd client && go build ./...`
- Desktop app: `cd desktop && wails dev`
- Mobile app: `cd mobile && flutter run`

Run tests: `make test` (see `Makefile`).

## Deployment

- `deploy/docker/docker-compose.yml` — single-host deployment
- `deploy/k8s/` — production Kubernetes manifests (HPA, Ingress, NetworkPolicy)
- `deploy/monitoring/` — Prometheus scrape config + Grafana dashboards
- `.github/workflows/` — CI (lint/test/build) and CD (image build + push)

## Security

See [`docs/architecture.md#security-model`](docs/architecture.md#security-model).
To report a vulnerability, see [`SECURITY.md`](SECURITY.md).

## License

MIT — see [`LICENSE`](LICENSE).
