DROP TRIGGER IF EXISTS trg_devices_updated_at ON devices;
DROP TRIGGER IF EXISTS trg_networks_updated_at ON networks;
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS error_logs;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS connection_logs;
DROP TABLE IF EXISTS relay_servers;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS network_members;
DROP TABLE IF EXISTS networks;
DROP TABLE IF EXISTS password_resets;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS audit_action;
DROP TYPE IF EXISTS connection_event_type;
DROP TYPE IF EXISTS device_os;
DROP TYPE IF EXISTS device_status;
DROP TYPE IF EXISTS network_role;
DROP TYPE IF EXISTS user_status;
