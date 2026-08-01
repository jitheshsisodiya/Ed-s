# NexusVPN Backend (control plane)

The control plane serves the REST API (`api/openapi.yaml`), the gRPC
`CoordinationService` (`proto/coordination/v1/coordination.proto`), a
WebSocket signaling endpoint, and a Prometheus metrics endpoint. State lives
in PostgreSQL (durable) and Redis (presence, pub/sub fanout, ICE rendezvous,
rate limiting).

## Layout (Clean Architecture)

```
cmd/api/                  entrypoint: config -> migrations -> servers -> graceful shutdown
internal/domain/          entities + repository interfaces (no framework dependencies)
internal/usecase/         application services (auth, network, device, coordination, logs, dashboard)
internal/repository/postgres/  pgx implementations of the domain repositories
internal/repository/redis/     presence, peer-event bus, ICE store, rate limiter
internal/transport/http/  REST handlers, JWT auth + CORS + metrics middleware, DTOs
internal/transport/grpc/  CoordinationService server + auth/metrics/logging interceptors
internal/transport/ws/    WebSocket hub for live presence/peer updates
internal/auth/            JWT, bcrypt, TOTP MFA, Google OAuth, relay session tokens
internal/config/          environment-based configuration
internal/logging/         zap structured logging, error_logs sink
internal/metrics/         Prometheus collectors
```

Dependencies point inward: `transport` → `usecase` → `domain`, with
`repository` implementing `domain`'s interfaces. The use-case layer never
imports HTTP or gRPC types.

## Configuration

All configuration comes from the environment (see `internal/config/config.go`).

| Variable | Default | Purpose |
|---|---|---|
| `HTTP_ADDR` | `:8080` | REST + WebSocket listen address |
| `GRPC_ADDR` | `:9090` | gRPC CoordinationService listen address |
| `METRICS_ADDR` | `:9091` | Prometheus `/metrics` listen address |
| `DATABASE_URL` | `postgres://nexusvpn:nexusvpn@localhost:5432/nexusvpn?sslmode=disable` | Postgres DSN |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | *(empty)* | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `JWT_ACCESS_SECRET` | dev default | HS256 secret for access tokens |
| `JWT_REFRESH_SECRET` | dev default | HS256 secret for refresh tokens |
| `JWT_ISSUER` | `nexusvpn-control-plane` | Expected `iss` claim |
| `ACCESS_TOKEN_TTL` | `15m` | Access token lifetime |
| `REFRESH_TOKEN_TTL` | `720h` | Refresh token lifetime |
| `RELAY_SESSION_SECRET` | dev default | Shared secret with relay nodes |
| `RELAY_SESSION_TTL` | `10m` | Relay session token lifetime |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` / `GOOGLE_REDIRECT_URL` | *(empty)* | Google OAuth; OAuth routes return 400 when unset |
| `PASSWORD_RESET_TTL` | `1h` | Password reset token lifetime |
| `PRESENCE_TTL` | `45s` | Redis presence key TTL |
| `CORS_ORIGINS` | `*` | Comma-separated allowed origins |
| `DEFAULT_DNS_SERVERS` | `1.1.1.1,8.8.8.8` | DNS handed to new networks |
| `ENVIRONMENT` | `development` | `production` refuses to start with default secrets |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

**Production safety**: with `ENVIRONMENT=production`, startup fails fast if
`JWT_ACCESS_SECRET`, `JWT_REFRESH_SECRET` or `RELAY_SESSION_SECRET` are still
at their development defaults.

## Running locally

```bash
# Dependencies (from the repo root)
docker compose -f deploy/docker/docker-compose.yml up -d postgres redis

# Then, from backend/
export DATABASE_URL='postgres://nexusvpn:nexusvpn@localhost:5432/nexusvpn?sslmode=disable'
export REDIS_ADDR='localhost:6379'
go run ./cmd/api
```

Migrations run automatically at startup from the embedded copy of
`db/migrations` (see `internal/repository/postgres/migrate.go`). The canonical
source is the repo-root `db/migrations/` directory.

## Tests

```bash
go test ./... -race            # everything
go test ./internal/usecase/ -v # business-logic unit tests (fake repositories)
go test ./internal/transport/... -v
```

The use-case tests run against in-memory fakes of the domain repository
interfaces, so no database is required. The gRPC transport tests spin up a
real in-process gRPC server over `bufconn` and exercise the wire contract
end to end (authentication, device registration, heartbeat/presence, relay
allocation, ICE rendezvous, and the `StreamPeerUpdates` server stream).

## Notable behaviors

- **Auth**: bcrypt password hashing, short-lived JWT access tokens, opaque
  refresh tokens stored SHA-256 hashed and rotated on every refresh (reuse of
  a rotated token is rejected). Password reset revokes all existing sessions.
- **MFA**: TOTP (RFC 6238); a generated secret is not active until a valid
  code is confirmed via `/auth/mfa/verify`.
- **Account enumeration**: `/auth/password/forgot` always returns `202`, and
  login failures return a single generic error whether or not the account
  exists. In non-production environments the reset token is echoed in the
  response so the flow is testable without SMTP.
- **Rate limiting**: `/auth/login`, `/auth/register` and
  `/auth/password/forgot` are each bounded twice — once by the thing being
  attacked (account or e-mail) and once by the address doing the attacking.
  A per-account limit alone does nothing about one password sprayed a single
  time across ten thousand accounts; a per-address limit alone punishes a
  whole office NAT for one careless colleague. The budgets are named
  constants at the top of `internal/usecase/auth_service.go`. A limiter
  outage **fails open**: Redis being down must not lock every user out of
  their own account, and that failure is already visible in the metrics for
  Redis itself. The forgot-password refusal is silent — returning an error
  would tell an attacker which addresses are worth grinding, which is the one
  thing that endpoint exists to never reveal.
- **Request bodies** are capped at 1 MiB. Every request this API accepts is a
  small JSON object, and the endpoints that most need the bound are the ones
  that run before any credential has been checked.
- **RBAC**: every network operation re-checks membership and role
  (`owner` > `admin` > `member`) in the use-case layer, not just in middleware.
- **Horizontal scaling**: no per-instance session state. Presence, peer-event
  fanout and ICE rendezvous all live in Redis, so any replica can serve any
  request and `StreamPeerUpdates`/WebSocket clients can attach to any pod.
- **Audit logging**: mutating actions write `audit_logs` rows asynchronously
  (fire-and-forget) so auditing never blocks or fails a request.
