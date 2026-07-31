# NexusVPN Relay

A relay node forwards WireGuard traffic between two peers when direct
peer-to-peer connectivity can't be established — typically when both sides
sit behind symmetric NATs that defeat UDP hole punching.

The relay **cannot read the traffic it carries**. WireGuard's Noise
handshake terminates on the two client devices; the relay only ever sees
ciphertext and forwards it byte-for-byte.

## How a session works

1. A client fails to hole-punch to a peer and calls the control plane's
   `RequestRelay` gRPC method.
2. The control plane picks the least-loaded active relay and returns its
   address plus a short-lived **session token** (HS256, signed with a secret
   shared only between the control plane and the relay fleet). The token
   names the device, the relay and the network.
3. The client sends a `BIND` frame carrying that token. The relay verifies
   the signature, checks the token was minted for *this* relay and for the
   device in the frame, then records the client's UDP address.
4. Once both peers have bound, each sends `DATA` frames naming the peer they
   want to reach. The relay looks up the peer's address within the session
   and forwards the payload untouched, rewriting the frame's device field to
   the *sender's* ID so the receiver learns the origin.
5. Sessions are evicted when idle past `SESSION_IDLE_TIMEOUT` or when a
   binding's token expires, forcing a re-bind.

A raw WireGuard datagram carries no addressing a relay can route on, which
is why `DATA` frames wrap the payload in a small header. This is the same
approach Tailscale's DERP relays use.

## Wire protocol

Every datagram begins with a one-byte frame type:

| Type | Name | Layout |
|---|---|---|
| `0x01` | `BIND` | `[type][16-byte device UUID][session token]` |
| `0x02` | `BIND_ACK` | `[type]` |
| `0x03` | `DATA` | `[type][16-byte peer UUID][opaque payload]` |
| `0x04` | `KEEPALIVE` | `[type]` |
| `0x05` | `ERROR` | `[type][utf-8 reason]` |

Unrecognized frames are dropped **without a reply**, so a relay node can't
be abused as a UDP reflection amplifier.

## Security properties

- **No forwarding without authentication.** A `DATA` frame from an address
  that has not completed a `BIND` is dropped and answered with an `ERROR`.
- **Tokens are relay-scoped.** After registration the node rejects tokens
  minted for a different relay, so a token can't be replayed across the fleet.
- **Tokens are device-scoped.** The device ID in a `BIND` frame must match
  the one inside the signed token.
- **Expiry is enforced continuously**, not just at bind time: a binding whose
  token has lapsed stops routing and is evicted.
- **Networks are isolated.** Peers can only reach devices bound to the same
  network's session.
- **No database credentials.** Relay nodes never talk to Postgres; they
  register and heartbeat through the control plane's internal HTTP endpoints
  using the shared secret.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `DATA_PORT` | `3479` | UDP port for relayed traffic |
| `CONTROL_PORT` | `3478` | Advertised control port (diagnostics) |
| `METRICS_PORT` | `9092` | Prometheus `/metrics` + `/healthz` |
| `BACKEND_URL` | `http://localhost:8080` | Control plane root URL |
| `RELAY_SESSION_SECRET` | dev default | Shared secret with the control plane |
| `RELAY_REGION` | `local` | Region label used for relay selection |
| `RELAY_PUBLIC_HOSTNAME` | `localhost` | Hostname clients are told to dial |
| `RELAY_CAPACITY` | `1000` | Max concurrent sessions |
| `SESSION_IDLE_TIMEOUT` | `5m` | Idle session eviction |
| `HEARTBEAT_INTERVAL` | `30s` | Load-report cadence |
| `ENVIRONMENT` | `development` | `production` refuses the default secret |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

`RELAY_SESSION_SECRET` **must** equal the control plane's value — it both
authenticates the register/heartbeat calls and verifies session tokens.

## Running

```bash
# With the stack from the repo root:
docker compose -f deploy/docker/docker-compose.yml up -d relay

# Or directly:
export BACKEND_URL=http://localhost:8080
export RELAY_SESSION_SECRET=<same as the backend>
export RELAY_PUBLIC_HOSTNAME=relay1.example.com
go run ./cmd/relayd
```

## Horizontal scaling

Relay nodes are **independent and share no state**. All session state is
derived from control-plane-signed tokens and lives only in the node's memory
for the life of the session; nothing is persisted and nothing is replicated.

Consequences for deployment:

- Add capacity by starting more nodes. Each registers itself on startup
  (idempotently, keyed on `hostname:port`) and reports its session count, so
  the control plane's `PickLeastLoaded` spreads new allocations automatically.
- A node can be drained by stopping it: clients whose sessions die simply
  request a new relay and re-bind, and the control plane stops selecting a
  node once its heartbeat goes stale.
- **Do not put relay nodes behind a UDP load balancer that reassigns
  backends per-datagram.** Both peers of a session must reach the *same*
  node, so each node needs a stable, individually routable address. This is
  why `deploy/k8s/21-relay.yaml` runs the fleet as a `DaemonSet` with
  `hostNetwork: true` rather than behind a Service.

## Metrics

Exposed on `METRICS_PORT`:

| Metric | Type | Meaning |
|---|---|---|
| `active_sessions` | gauge | Sessions currently held by this node |
| `relay_bound_devices` | gauge | Devices bound across all sessions |
| `bytes_relayed_total` | counter | Bytes forwarded between peers |
| `relay_packets_relayed_total` | counter | Datagrams forwarded |
| `relay_packets_dropped_total` | counter | Datagrams refused (unauthenticated, unroutable, malformed) |

## Tests

```bash
go test ./... -race
```

The relay tests run a real server on a loopback UDP socket and drive it with
real clients, covering the full bind/forward path, byte-exact payload
delivery, and the rejection paths (unauthenticated senders, forged and
expired tokens, device mismatch, cross-network isolation, NAT rebinding,
idle eviction and capacity limits).
