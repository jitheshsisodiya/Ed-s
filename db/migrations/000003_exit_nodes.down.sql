DROP INDEX IF EXISTS idx_devices_exit_nodes;
ALTER TABLE devices DROP COLUMN IF EXISTS advertises_exit_node;
