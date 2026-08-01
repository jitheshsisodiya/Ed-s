package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// DeviceRepo is a pgx-backed implementation of domain.DeviceRepository.
type DeviceRepo struct {
	db Querier
}

// NewDeviceRepo builds a DeviceRepo.
func NewDeviceRepo(db Querier) *DeviceRepo {
	return &DeviceRepo{db: db}
}

const deviceColumns = `id, user_id, network_id, name, os, os_version, public_key, virtual_ip::text,
	last_public_ip::text, last_private_ip::text, nat_type, status, last_seen_at, last_handshake_at,
	bytes_sent, bytes_received, advertises_exit_node, created_at, updated_at`

func scanDevice(row interface{ Scan(dest ...any) error }) (*domain.Device, error) {
	var d domain.Device
	if err := row.Scan(
		&d.ID, &d.UserID, &d.NetworkID, &d.Name, &d.OS, &d.OSVersion, &d.PublicKey, &d.VirtualIP,
		&d.LastPublicIP, &d.LastPrivateIP, &d.NATType, &d.Status, &d.LastSeenAt, &d.LastHandshakeAt,
		&d.BytesSent, &d.BytesReceived, &d.AdvertisesExitNode, &d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, translateErr(err)
	}
	return &d, nil
}

// Create inserts a new device row.
func (r *DeviceRepo) Create(ctx context.Context, d *domain.Device) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO devices (id, user_id, network_id, name, os, os_version, public_key, virtual_ip, status, advertises_exit_node)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::inet, $9, $10)
		RETURNING `+deviceColumns,
		orNewID(d.ID), d.UserID, d.NetworkID, d.Name, d.OS, d.OSVersion, d.PublicKey, d.VirtualIP, d.Status,
		d.AdvertisesExitNode,
	)
	saved, err := scanDevice(row)
	if err != nil {
		return err
	}
	*d = *saved
	return nil
}

// GetByID fetches a device by primary key.
func (r *DeviceRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Device, error) {
	row := r.db.QueryRow(ctx, `SELECT `+deviceColumns+` FROM devices WHERE id = $1`, id)
	return scanDevice(row)
}

// GetByPublicKey fetches a device by its (globally unique) WireGuard public key.
func (r *DeviceRepo) GetByPublicKey(ctx context.Context, publicKey string) (*domain.Device, error) {
	row := r.db.QueryRow(ctx, `SELECT `+deviceColumns+` FROM devices WHERE public_key = $1`, publicKey)
	return scanDevice(row)
}

// GetByNetworkAndUser fetches the (first) device a user has on a network.
func (r *DeviceRepo) GetByNetworkAndUser(ctx context.Context, networkID, userID uuid.UUID) (*domain.Device, error) {
	row := r.db.QueryRow(ctx, `SELECT `+deviceColumns+` FROM devices WHERE network_id = $1 AND user_id = $2 ORDER BY created_at ASC LIMIT 1`, networkID, userID)
	return scanDevice(row)
}

// ListByNetwork returns every device on a network.
func (r *DeviceRepo) ListByNetwork(ctx context.Context, networkID uuid.UUID) ([]*domain.Device, error) {
	return r.queryDevices(ctx, `SELECT `+deviceColumns+` FROM devices WHERE network_id = $1 ORDER BY created_at ASC`, networkID)
}

// ListByUser returns every device a user owns.
func (r *DeviceRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*domain.Device, error) {
	return r.queryDevices(ctx, `SELECT `+deviceColumns+` FROM devices WHERE user_id = $1 ORDER BY created_at ASC`, userID)
}

// ListPeers returns every other device on the same network as deviceID.
func (r *DeviceRepo) ListPeers(ctx context.Context, deviceID uuid.UUID) ([]*domain.Device, error) {
	return r.queryDevices(ctx, `
		SELECT `+deviceColumns+` FROM devices
		WHERE network_id = (SELECT network_id FROM devices WHERE id = $1) AND id <> $1
		ORDER BY created_at ASC`, deviceID)
}

func (r *DeviceRepo) queryDevices(ctx context.Context, sql string, args ...any) ([]*domain.Device, error) {
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.Device
	for rows.Next() {
		var d domain.Device
		if err := rows.Scan(
			&d.ID, &d.UserID, &d.NetworkID, &d.Name, &d.OS, &d.OSVersion, &d.PublicKey, &d.VirtualIP,
			&d.LastPublicIP, &d.LastPrivateIP, &d.NATType, &d.Status, &d.LastSeenAt, &d.LastHandshakeAt,
			&d.BytesSent, &d.BytesReceived, &d.AdvertisesExitNode, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, translateErr(err)
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// Update persists name/os/os_version changes to an existing device row.
func (r *DeviceRepo) Update(ctx context.Context, d *domain.Device) error {
	row := r.db.QueryRow(ctx, `
		UPDATE devices SET name = $2, os = $3, os_version = $4, advertises_exit_node = $5
		WHERE id = $1
		RETURNING `+deviceColumns,
		d.ID, d.Name, d.OS, d.OSVersion, d.AdvertisesExitNode,
	)
	saved, err := scanDevice(row)
	if err != nil {
		return err
	}
	*d = *saved
	return nil
}

// UpdateHeartbeat updates presence/status/endpoint/counters for a device.
func (r *DeviceRepo) UpdateHeartbeat(ctx context.Context, id uuid.UUID, status domain.DeviceStatus, lastPublicIP, lastPrivateIP, natType *string, bytesSent, bytesReceived int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE devices SET
			status = $2,
			last_public_ip = COALESCE(NULLIF($3, '')::inet, last_public_ip),
			last_private_ip = COALESCE(NULLIF($4, '')::inet, last_private_ip),
			nat_type = COALESCE(NULLIF($5, ''), nat_type),
			last_seen_at = now(),
			last_handshake_at = now(),
			bytes_sent = bytes_sent + $6,
			bytes_received = bytes_received + $7
		WHERE id = $1`,
		id, status, derefOrEmpty(lastPublicIP), derefOrEmpty(lastPrivateIP), derefOrEmpty(natType), bytesSent, bytesReceived,
	)
	return translateErr(err)
}

// Delete removes a device row.
func (r *DeviceRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	if err != nil {
		return translateErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// NextFreeVirtualIP scans cidr's host range and returns the lowest address
// not already assigned to a device on networkID.
func (r *DeviceRepo) NextFreeVirtualIP(ctx context.Context, networkID uuid.UUID, cidr string) (string, error) {
	rows, err := r.db.Query(ctx, `SELECT virtual_ip::text FROM devices WHERE network_id = $1`, networkID)
	if err != nil {
		return "", translateErr(err)
	}
	defer rows.Close()

	used := map[string]struct{}{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			return "", translateErr(err)
		}
		used[ip] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return "", translateErr(err)
	}

	ip, err := nextFreeHostIP(cidr, used)
	if err != nil {
		return "", domain.ErrNetworkFull
	}
	return ip, nil
}

// CountDistinctActiveUsers counts distinct users with a device seen since t.
func (r *DeviceRepo) CountDistinctActiveUsers(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(DISTINCT user_id) FROM devices WHERE last_seen_at >= $1`, since).Scan(&n)
	return n, translateErr(err)
}

// CountDistinctActiveNetworks counts distinct networks with a device seen since t.
func (r *DeviceRepo) CountDistinctActiveNetworks(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(DISTINCT network_id) FROM devices WHERE last_seen_at >= $1`, since).Scan(&n)
	return n, translateErr(err)
}

// CountTotal counts every device row.
func (r *DeviceRepo) CountTotal(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&n)
	return n, translateErr(err)
}
