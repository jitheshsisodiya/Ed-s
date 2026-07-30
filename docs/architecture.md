# NexusVPN Architecture

NexusVPN is a self-hostable, open-source virtual LAN / mesh VPN platform in
the spirit of Radmin VPN / Hamachi / Tailscale: a lightweight control plane
issues identity, network membership and peer topology, while data traffic
flows directly between peers over WireGuard whenever NAT allows it, and
falls back to relay servers otherwise.

## Components

| Component | Path | Language | Responsibility |
|---|---|---|---|
| Backend (control plane) | `backend/` | Go | Auth, network/device management, REST+gRPC+WebSocket API, coordination |
| Relay | `relay/` | Go | Stateless UDP relay (TURN-like) for peers that can't establish direct P2P |
| Client core | `client/` | Go | Cross-platform agent: WireGuard interface, STUN/ICE, hole punching, relay client |
| Desktop app | `desktop/` | Go + Wails + React/TS | GUI wrapper around client core for Windows/Windows Server/Linux/macOS |
| Mobile app | `mobile/` | Flutter/Dart | Android/iOS app talking to the same REST API + a platform VPN tunnel service |
| Admin panel | `admin/` | React + TS + Tailwind | Network/device/user administration, dashboards, audit logs |
| SDKs | `sdk/` | Go, TypeScript | Typed clients generated/hand-written against `api/openapi.yaml` |

## High-level architecture

```mermaid
flowchart LR
    subgraph Clients
        Desktop["Desktop App (Wails)"]
        Mobile["Mobile App (Flutter)"]
        AdminUI["Admin Panel (React)"]
    end

    subgraph ControlPlane["Control Plane (backend/)"]
        REST["REST API"]
        GRPC["gRPC Coordination Service"]
        WS["WebSocket Signaling"]
        Auth["Auth Service (JWT/OAuth/MFA)"]
        NetSvc["Network Service"]
        DevSvc["Device Service"]
    end

    PG[(PostgreSQL)]
    Redis[(Redis - presence/pubsub)]

    subgraph RelayCluster["Relay Cluster (relay/)"]
        Relay1["Relay Node A"]
        Relay2["Relay Node B"]
    end

    Desktop -- HTTPS/JSON --> REST
    Desktop -- gRPC/TLS --> GRPC
    Desktop -- WSS --> WS
    Mobile -- HTTPS/JSON --> REST
    Mobile -- gRPC/TLS --> GRPC
    AdminUI -- HTTPS/JSON --> REST

    REST --> Auth
    REST --> NetSvc
    REST --> DevSvc
    GRPC --> DevSvc
    WS --> DevSvc

    Auth --> PG
    NetSvc --> PG
    DevSvc --> PG
    DevSvc --> Redis
    WS --> Redis

    Desktop -. "Direct WireGuard UDP (P2P, when NAT allows)" .-> Mobile
    Desktop -. "Relayed WireGuard UDP" .-> Relay1
    Relay1 -. relayed .-> Mobile
    DevSvc -- "relay allocation" --> RelayCluster
```

## Connectivity strategy

1. Client registers device + public key with control plane, gets an
   assigned virtual IP inside the network's CIDR.
2. Client performs STUN binding requests against configured STUN servers to
   discover its public (server-reflected) endpoint and classify NAT type
   (open / full-cone / restricted / symmetric).
3. Client reports its endpoint(s) via gRPC `Heartbeat` / gets peer endpoints
   via `StreamPeerUpdates`.
4. For each peer, the client attempts simultaneous UDP hole punching
   (ICE-style candidate exchange via `ExchangeICECandidates`) directly to
   the peer's WireGuard listen port.
5. If direct P2P handshake doesn't complete within a timeout (symmetric NAT
   on both sides, restrictive firewalls), the client calls `RequestRelay`
   and reconfigures the WireGuard peer's endpoint to point at the assigned
   relay node, which forwards encrypted UDP datagrams between the two
   peers without ever decrypting them (WireGuard's own crypto is
   end-to-end; the relay only sees ciphertext).
6. The client continuously monitors handshake freshness and automatically
   retries direct connectivity, falls back to relay, and reconnects on
   network changes (Wi-Fi/cellular switch, sleep/wake).

## Entity relationship diagram

```mermaid
erDiagram
    USERS ||--o{ REFRESH_TOKENS : has
    USERS ||--o{ NETWORKS : owns
    USERS ||--o{ NETWORK_MEMBERS : "is member"
    NETWORKS ||--o{ NETWORK_MEMBERS : has
    NETWORKS ||--o{ DEVICES : contains
    USERS ||--o{ DEVICES : registers
    NETWORKS ||--o{ AUDIT_LOGS : scopes
    NETWORKS ||--o{ CONNECTION_LOGS : scopes
    DEVICES ||--o{ CONNECTION_LOGS : generates
    RELAY_SERVERS ||--o{ CONNECTION_LOGS : "relays via"

    USERS {
        uuid id PK
        citext email
        text password_hash
        text display_name
        user_status status
        bool mfa_enabled
    }
    NETWORKS {
        uuid id PK
        text name
        cidr cidr
        uuid owner_id FK
        text invite_code
    }
    NETWORK_MEMBERS {
        uuid id PK
        uuid network_id FK
        uuid user_id FK
        network_role role
    }
    DEVICES {
        uuid id PK
        uuid user_id FK
        uuid network_id FK
        text public_key
        inet virtual_ip
        device_status status
    }
    RELAY_SERVERS {
        uuid id PK
        text region
        text hostname
        int capacity
    }
    CONNECTION_LOGS {
        uuid id PK
        uuid network_id FK
        uuid device_id FK
        connection_event_type event_type
    }
    AUDIT_LOGS {
        uuid id PK
        uuid actor_user_id FK
        audit_action action
    }
```

## Sequence: device join + P2P handshake with relay fallback

```mermaid
sequenceDiagram
    participant A as Device A (client)
    participant CP as Control Plane
    participant B as Device B (client)
    participant R as Relay Node

    A->>CP: RegisterDevice(pubkey, networkId, inviteCode-derived JWT)
    CP-->>A: virtual IP, existing peers, network CIDR
    A->>CP: Heartbeat(publicEndpoint, natType)
    CP-->>B: StreamPeerUpdates: PEER_JOINED(A)
    B->>CP: ExchangeICECandidates(target=A, candidates)
    CP-->>A: ExchangeICECandidates response (B's candidates)
    A->>B: UDP hole-punch WireGuard handshake (direct)
    alt Direct handshake succeeds
        A->>B: Encrypted WireGuard traffic (P2P)
    else Direct handshake times out (symmetric NAT)
        A->>CP: RequestRelay(networkId)
        CP-->>A: relay endpoint + session token
        B->>CP: RequestRelay(networkId)
        CP-->>B: relay endpoint + session token
        A->>R: Encrypted WireGuard traffic
        R->>B: Forwarded encrypted WireGuard traffic
    end
    A->>CP: Heartbeat(bytesSent, bytesReceived) [periodic]
```

## Deployment diagram

```mermaid
flowchart TB
    subgraph Internet
        U1[Desktop/Mobile Clients]
    end

    subgraph K8sCluster["Kubernetes Cluster"]
        subgraph IngressNS["ingress-nginx"]
            Ingress[Ingress / TLS termination]
        end
        subgraph AppNS["nexusvpn namespace"]
            BackendDeploy["backend Deployment (HPA, 3-10 replicas)"]
            RelayDeploy["relay Deployment (DaemonSet-like, hostNetwork)"]
            AdminDeploy["admin Deployment"]
        end
        subgraph DataNS["data namespace"]
            PGPrimary[(Postgres Primary)]
            PGReplica[(Postgres Replica)]
            RedisCluster[(Redis Cluster)]
        end
        subgraph ObsNS["observability namespace"]
            Prom[Prometheus]
            Graf[Grafana]
        end
    end

    U1 -->|HTTPS/WSS| Ingress
    U1 -->|UDP WireGuard| RelayDeploy
    Ingress --> BackendDeploy
    Ingress --> AdminDeploy
    BackendDeploy --> PGPrimary
    PGPrimary --> PGReplica
    BackendDeploy --> RedisCluster
    BackendDeploy -.metrics.-> Prom
    RelayDeploy -.metrics.-> Prom
    Prom --> Graf
```

## Security model

- **Transport**: control-plane REST/gRPC/WS over TLS 1.3. Data plane uses
  WireGuard (Curve25519 for key exchange, ChaCha20-Poly1305 for AEAD).
- **Identity**: JWT access tokens (short-lived, 15 min) + rotating refresh
  tokens (stored hashed, revocable, device-bound).
- **Device keys**: every device generates its own Curve25519 keypair
  locally; the private key never leaves the device. Only the public key is
  registered with the control plane.
- **Certificate/key rotation**: device WireGuard keys can be rotated
  on-demand or on a schedule; rotation invalidates the old public key
  network-wide via the peer update stream.
- **MFA**: TOTP (RFC 6238), enforced optionally per-account.
- **Least privilege**: network roles (`owner`, `admin`, `member`) gate
  invite rotation, member removal, and device management via RBAC
  middleware on every mutating endpoint.
