-- Exit nodes: a device can offer to carry other devices' internet traffic.
--
-- This is opt-in per device and stored on the device rather than the
-- network, because willingness is a property of one machine and its owner's
-- bandwidth — a laptop on a metered connection and a home server on fibre
-- belong to the same network and should answer this question differently.
--
-- Advertising is not the same as being used: a peer still has to choose this
-- device, and that choice lives on the client. The column only says the
-- offer is open.
ALTER TABLE devices
    ADD COLUMN advertises_exit_node BOOLEAN NOT NULL DEFAULT false;

-- Clients ask "which devices on this network can I route through", which is
-- a small subset of a network's devices, so the index is partial.
CREATE INDEX idx_devices_exit_nodes
    ON devices (network_id)
    WHERE advertises_exit_node;
