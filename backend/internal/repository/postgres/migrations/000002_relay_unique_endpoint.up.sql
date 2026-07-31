-- A relay node re-registers itself on every startup; a unique key on its
-- public endpoint lets that registration be an idempotent upsert instead of
-- accumulating duplicate rows.
ALTER TABLE relay_servers
    ADD CONSTRAINT uq_relay_servers_endpoint UNIQUE (hostname, relay_port);
