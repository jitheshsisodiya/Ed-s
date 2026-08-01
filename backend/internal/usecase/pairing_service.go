package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// PairingService enrols a second device onto an account without that account's
// password being typed on it.
//
// The device that already has a session shows a code; the new one reads it and
// is signed in. It exists because the alternative on a phone is typing a server
// address, an email and a password on a touchscreen, and the address in
// particular is not something most people can be expected to know — it is
// whatever private IP the router handed their laptop this week.
type PairingService struct {
	users   domain.UserRepository
	members domain.NetworkMemberRepository
	pairing *auth.PairingManager
	issuer  tokenPairIssuer
	audit   *AuditRecorder
}

// tokenPairIssuer is the half of AuthService this needs: turning a user into
// a session. Narrow on purpose — pairing has no business reaching passwords,
// MFA or OAuth.
type tokenPairIssuer interface {
	IssueTokenPairFor(ctx context.Context, u *domain.User, ipAddress, userAgent string) (*AuthResult, error)
}

// NewPairingService builds a PairingService.
func NewPairingService(
	users domain.UserRepository,
	members domain.NetworkMemberRepository,
	pairing *auth.PairingManager,
	issuer tokenPairIssuer,
	audit *AuditRecorder,
) *PairingService {
	return &PairingService{users: users, members: members, pairing: pairing, issuer: issuer, audit: audit}
}

// Pairing is a freshly minted code and how long it is good for.
type Pairing struct {
	Token     string
	ExpiresAt time.Time
	ExpiresIn int
}

// Start issues a pairing code for an already-authenticated user, optionally
// naming the network the new device should land on.
//
// The network is checked here rather than at claim time, so a code is never
// handed out pointing at a network its own issuer cannot use.
func (s *PairingService) Start(ctx context.Context, userID uuid.UUID, networkID string) (*Pairing, error) {
	if networkID != "" {
		nid, err := uuid.Parse(networkID)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if _, err := s.members.Get(ctx, nid, userID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrForbidden
			}
			return nil, err
		}
	}

	token, expires, err := s.pairing.Issue(userID, networkID)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, &userID, nil, domain.AuditPairingStarted, "user", userID.String(), "", nil)

	return &Pairing{
		Token:     token,
		ExpiresAt: expires,
		ExpiresIn: int(s.pairing.TTL().Seconds()),
	}, nil
}

// ClaimedPairing is a session, plus where the new device should go next.
type ClaimedPairing struct {
	Auth      *AuthResult
	NetworkID string
	Email     string
}

// Claim spends a pairing code and returns a session for the account that
// issued it.
//
// Deliberately not rate limited by address: the code is single use and lives
// for minutes, so guessing is not the threat — and a limit keyed on the
// address would lock out the one legitimate device the moment somebody else
// on the same network fumbled a scan.
func (s *PairingService) Claim(ctx context.Context, token, ipAddress, userAgent string) (*ClaimedPairing, error) {
	claims, err := s.pairing.Consume(token)
	if err != nil {
		// Used, expired, forged and malformed are all reported the same way.
		// Distinguishing them would tell somebody holding a stolen code
		// whether it was ever real.
		return nil, domain.ErrUnauthorized
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, domain.ErrUnauthorized
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		// The account was deleted between the code being shown and read.
		return nil, domain.ErrUnauthorized
	}

	result, err := s.issuer.IssueTokenPairFor(ctx, u, ipAddress, userAgent)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, &userID, nil, domain.AuditPairingClaimed, "user", userID.String(), ipAddress, nil)

	return &ClaimedPairing{Auth: result, NetworkID: claims.NetworkID, Email: u.Email}, nil
}
