package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// NetworkRepo is a pgx-backed implementation of domain.NetworkRepository.
type NetworkRepo struct {
	db Querier
}

// NewNetworkRepo builds a NetworkRepo.
func NewNetworkRepo(db Querier) *NetworkRepo {
	return &NetworkRepo{db: db}
}

const networkColumns = `id, name, description, cidr::text, cidr_v6::text, dns_servers::text[], owner_id, invite_code, invite_code_expires_at, invite_enabled, allow_broadcast, created_at, updated_at`

func scanNetwork(row interface{ Scan(dest ...any) error }) (*domain.Network, error) {
	var n domain.Network
	if err := row.Scan(
		&n.ID, &n.Name, &n.Description, &n.CIDR, &n.CIDRv6, &n.DNSServers,
		&n.OwnerID, &n.InviteCode, &n.InviteCodeExpiresAt, &n.InviteEnabled, &n.AllowBroadcast,
		&n.CreatedAt, &n.UpdatedAt,
	); err != nil {
		return nil, translateErr(err)
	}
	return &n, nil
}

// Create inserts a new network row.
func (r *NetworkRepo) Create(ctx context.Context, n *domain.Network) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO networks (id, name, description, cidr, cidr_v6, dns_servers, owner_id, invite_code, invite_code_expires_at, invite_enabled, allow_broadcast)
		VALUES ($1, $2, $3, $4::cidr, NULLIF($5, '')::cidr, $6::inet[], $7, $8, $9, $10, $11)
		RETURNING `+networkColumns,
		orNewID(n.ID), n.Name, n.Description, n.CIDR, derefOrEmpty(n.CIDRv6), n.DNSServers,
		n.OwnerID, n.InviteCode, n.InviteCodeExpiresAt, n.InviteEnabled, n.AllowBroadcast,
	)
	saved, err := scanNetwork(row)
	if err != nil {
		return err
	}
	*n = *saved
	return nil
}

func derefOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GetByID fetches a network by primary key.
func (r *NetworkRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Network, error) {
	row := r.db.QueryRow(ctx, `SELECT `+networkColumns+` FROM networks WHERE id = $1`, id)
	return scanNetwork(row)
}

// GetByInviteCode fetches a network by its current invite code.
func (r *NetworkRepo) GetByInviteCode(ctx context.Context, code string) (*domain.Network, error) {
	row := r.db.QueryRow(ctx, `SELECT `+networkColumns+` FROM networks WHERE invite_code = $1`, code)
	return scanNetwork(row)
}

// ListForUser returns every network a user is a member of, with their role.
func (r *NetworkRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]*domain.Network, error) {
	rows, err := r.db.Query(ctx, `
		SELECT n.id, n.name, n.description, n.cidr::text, n.cidr_v6::text, n.dns_servers::text[],
		       n.owner_id, n.invite_code, n.invite_code_expires_at, n.invite_enabled, n.allow_broadcast,
		       n.created_at, n.updated_at, m.role
		FROM networks n
		JOIN network_members m ON m.network_id = n.id
		WHERE m.user_id = $1
		ORDER BY n.created_at DESC`, userID)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()

	var out []*domain.Network
	for rows.Next() {
		var n domain.Network
		if err := rows.Scan(
			&n.ID, &n.Name, &n.Description, &n.CIDR, &n.CIDRv6, &n.DNSServers,
			&n.OwnerID, &n.InviteCode, &n.InviteCodeExpiresAt, &n.InviteEnabled, &n.AllowBroadcast,
			&n.CreatedAt, &n.UpdatedAt, &n.CallerRole,
		); err != nil {
			return nil, translateErr(err)
		}
		out = append(out, &n)
	}
	return out, rows.Err()
}

// Update persists changes to an existing network row.
func (r *NetworkRepo) Update(ctx context.Context, n *domain.Network) error {
	row := r.db.QueryRow(ctx, `
		UPDATE networks SET
			name = $2, description = $3, cidr = $4::cidr, dns_servers = $5::inet[]
		WHERE id = $1
		RETURNING `+networkColumns,
		n.ID, n.Name, n.Description, n.CIDR, n.DNSServers,
	)
	saved, err := scanNetwork(row)
	if err != nil {
		return err
	}
	*n = *saved
	return nil
}

// Delete removes a network (cascades to members/devices/logs via FKs).
func (r *NetworkRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM networks WHERE id = $1`, id)
	if err != nil {
		return translateErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// RotateInviteCode replaces a network's invite code and optionally sets an expiry.
func (r *NetworkRepo) RotateInviteCode(ctx context.Context, id uuid.UUID, newCode string, expiresAt *time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE networks SET invite_code = $2, invite_code_expires_at = $3 WHERE id = $1`, id, newCode, expiresAt)
	return translateErr(err)
}

// CountMembers returns the number of members on a network.
func (r *NetworkRepo) CountMembers(ctx context.Context, networkID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM network_members WHERE network_id = $1`, networkID).Scan(&n)
	return n, translateErr(err)
}

// CountDevices returns the number of devices on a network.
func (r *NetworkRepo) CountDevices(ctx context.Context, networkID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE network_id = $1`, networkID).Scan(&n)
	return n, translateErr(err)
}
