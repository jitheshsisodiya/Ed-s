package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// NetworkMemberRepo is a pgx-backed implementation of domain.NetworkMemberRepository.
type NetworkMemberRepo struct {
	db Querier
}

// NewNetworkMemberRepo builds a NetworkMemberRepo.
func NewNetworkMemberRepo(db Querier) *NetworkMemberRepo {
	return &NetworkMemberRepo{db: db}
}

// Create inserts a new network_members row.
func (r *NetworkMemberRepo) Create(ctx context.Context, m *domain.NetworkMember) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO network_members (id, network_id, user_id, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, network_id, user_id, role, joined_at`,
		orNewID(m.ID), m.NetworkID, m.UserID, m.Role,
	)
	var saved domain.NetworkMember
	if err := row.Scan(&saved.ID, &saved.NetworkID, &saved.UserID, &saved.Role, &saved.JoinedAt); err != nil {
		return translateErr(err)
	}
	*m = saved
	return nil
}

// Get fetches a single membership row.
func (r *NetworkMemberRepo) Get(ctx context.Context, networkID, userID uuid.UUID) (*domain.NetworkMember, error) {
	row := r.db.QueryRow(ctx, `
		SELECT m.id, m.network_id, m.user_id, m.role, m.joined_at, u.email, u.display_name
		FROM network_members m JOIN users u ON u.id = m.user_id
		WHERE m.network_id = $1 AND m.user_id = $2`, networkID, userID)
	var m domain.NetworkMember
	if err := row.Scan(&m.ID, &m.NetworkID, &m.UserID, &m.Role, &m.JoinedAt, &m.Email, &m.DisplayName); err != nil {
		return nil, translateErr(err)
	}
	return &m, nil
}

// List returns every member of a network, joined with user profile info.
func (r *NetworkMemberRepo) List(ctx context.Context, networkID uuid.UUID) ([]*domain.NetworkMember, error) {
	rows, err := r.db.Query(ctx, `
		SELECT m.id, m.network_id, m.user_id, m.role, m.joined_at, u.email, u.display_name
		FROM network_members m JOIN users u ON u.id = m.user_id
		WHERE m.network_id = $1
		ORDER BY m.joined_at ASC`, networkID)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()

	var out []*domain.NetworkMember
	for rows.Next() {
		var m domain.NetworkMember
		if err := rows.Scan(&m.ID, &m.NetworkID, &m.UserID, &m.Role, &m.JoinedAt, &m.Email, &m.DisplayName); err != nil {
			return nil, translateErr(err)
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

// ListNetworkIDsForUser returns every network ID a user belongs to.
func (r *NetworkMemberRepo) ListNetworkIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT network_id FROM network_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, translateErr(err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// UpdateRole changes a member's role.
func (r *NetworkMemberRepo) UpdateRole(ctx context.Context, networkID, userID uuid.UUID, role domain.NetworkRole) error {
	tag, err := r.db.Exec(ctx, `UPDATE network_members SET role = $3 WHERE network_id = $1 AND user_id = $2`, networkID, userID, role)
	if err != nil {
		return translateErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Delete removes a membership row.
func (r *NetworkMemberRepo) Delete(ctx context.Context, networkID, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM network_members WHERE network_id = $1 AND user_id = $2`, networkID, userID)
	if err != nil {
		return translateErr(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountByRole counts members of a network with a given role.
func (r *NetworkMemberRepo) CountByRole(ctx context.Context, networkID uuid.UUID, role domain.NetworkRole) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM network_members WHERE network_id = $1 AND role = $2`, networkID, role).Scan(&n)
	return n, translateErr(err)
}
