-- NexusVPN core schema
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";

CREATE TYPE user_status AS ENUM ('active', 'disabled', 'pending_verification');
CREATE TYPE network_role AS ENUM ('owner', 'admin', 'member');
CREATE TYPE device_status AS ENUM ('online', 'offline', 'unknown');
CREATE TYPE device_os AS ENUM ('windows', 'windows_server', 'linux', 'macos', 'android', 'ios', 'unknown');
CREATE TYPE connection_event_type AS ENUM ('connect', 'disconnect', 'p2p_established', 'relay_fallback', 'nat_traversal_failed', 'reconnect');
CREATE TYPE audit_action AS ENUM (
  'user.register', 'user.login', 'user.login_failed', 'user.password_reset', 'user.mfa_enabled', 'user.mfa_disabled',
  'network.create', 'network.update', 'network.delete', 'network.invite_created', 'network.invite_rotated',
  'network.member_joined', 'network.member_removed', 'network.member_role_changed',
  'device.registered', 'device.removed', 'device.key_rotated'
);

CREATE TABLE users (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email               CITEXT UNIQUE NOT NULL,
    password_hash       TEXT NOT NULL,
    display_name        TEXT NOT NULL,
    status              user_status NOT NULL DEFAULT 'pending_verification',
    mfa_secret          TEXT,
    mfa_enabled         BOOLEAN NOT NULL DEFAULT FALSE,
    oauth_provider      TEXT,
    oauth_subject       TEXT,
    email_verified_at   TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (oauth_provider, oauth_subject)
);

CREATE TABLE refresh_tokens (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash     TEXT NOT NULL UNIQUE,
    user_agent     TEXT,
    ip_address     INET,
    expires_at     TIMESTAMPTZ NOT NULL,
    revoked_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);

CREATE TABLE password_resets (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash   TEXT NOT NULL UNIQUE,
    expires_at   TIMESTAMPTZ NOT NULL,
    used_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE networks (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                   TEXT NOT NULL,
    description            TEXT NOT NULL DEFAULT '',
    cidr                   CIDR NOT NULL,
    cidr_v6                CIDR,
    dns_servers            INET[] NOT NULL DEFAULT '{}',
    owner_id               UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    invite_code            TEXT UNIQUE NOT NULL,
    invite_code_expires_at TIMESTAMPTZ,
    invite_enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    allow_broadcast        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_networks_owner_id ON networks(owner_id);
CREATE INDEX idx_networks_invite_code ON networks(invite_code);

CREATE TABLE network_members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    network_id  UUID NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        network_role NOT NULL DEFAULT 'member',
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (network_id, user_id)
);
CREATE INDEX idx_network_members_user_id ON network_members(user_id);
CREATE INDEX idx_network_members_network_id ON network_members(network_id);

CREATE TABLE devices (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    network_id       UUID NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    name             TEXT NOT NULL,
    os               device_os NOT NULL DEFAULT 'unknown',
    os_version       TEXT NOT NULL DEFAULT '',
    public_key       TEXT NOT NULL UNIQUE,
    virtual_ip       INET NOT NULL,
    last_public_ip   INET,
    last_private_ip  INET,
    nat_type         TEXT,
    status           device_status NOT NULL DEFAULT 'unknown',
    last_seen_at     TIMESTAMPTZ,
    last_handshake_at TIMESTAMPTZ,
    bytes_sent       BIGINT NOT NULL DEFAULT 0,
    bytes_received   BIGINT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (network_id, virtual_ip)
);
CREATE INDEX idx_devices_network_id ON devices(network_id);
CREATE INDEX idx_devices_user_id ON devices(user_id);
CREATE INDEX idx_devices_status ON devices(status);

CREATE TABLE relay_servers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    region         TEXT NOT NULL,
    hostname       TEXT NOT NULL,
    public_key     TEXT NOT NULL,
    control_port   INTEGER NOT NULL DEFAULT 3478,
    relay_port     INTEGER NOT NULL DEFAULT 3479,
    capacity       INTEGER NOT NULL DEFAULT 1000,
    current_load   INTEGER NOT NULL DEFAULT 0,
    status         TEXT NOT NULL DEFAULT 'active',
    last_heartbeat_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE connection_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    network_id      UUID NOT NULL REFERENCES networks(id) ON DELETE CASCADE,
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    peer_device_id  UUID REFERENCES devices(id) ON DELETE SET NULL,
    relay_server_id UUID REFERENCES relay_servers(id) ON DELETE SET NULL,
    event_type      connection_event_type NOT NULL,
    latency_ms      INTEGER,
    bytes_sent      BIGINT NOT NULL DEFAULT 0,
    bytes_received  BIGINT NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_connection_logs_network_id ON connection_logs(network_id, created_at DESC);
CREATE INDEX idx_connection_logs_device_id ON connection_logs(device_id, created_at DESC);

CREATE TABLE audit_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    network_id      UUID REFERENCES networks(id) ON DELETE CASCADE,
    action          audit_action NOT NULL,
    target_type     TEXT,
    target_id       TEXT,
    ip_address      INET,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_logs_network_id ON audit_logs(network_id, created_at DESC);
CREATE INDEX idx_audit_logs_actor ON audit_logs(actor_user_id, created_at DESC);

CREATE TABLE error_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service     TEXT NOT NULL,
    level       TEXT NOT NULL,
    message     TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_error_logs_service_created ON error_logs(service, created_at DESC);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_networks_updated_at BEFORE UPDATE ON networks FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_devices_updated_at BEFORE UPDATE ON devices FOR EACH ROW EXECUTE FUNCTION set_updated_at();
