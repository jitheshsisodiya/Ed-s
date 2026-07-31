package domain

import "errors"

// Sentinel domain errors. Transport layers translate these into the
// appropriate HTTP status codes / gRPC status codes.
var (
	ErrNotFound           = errors.New("resource not found")
	ErrAlreadyExists      = errors.New("resource already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrInvalidInput       = errors.New("invalid input")
	ErrMFARequired        = errors.New("mfa code required")
	ErrInvalidMFACode     = errors.New("invalid mfa code")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenRevoked       = errors.New("token revoked")
	ErrTokenInvalid       = errors.New("token invalid")
	ErrInviteInvalid      = errors.New("invite code invalid or expired")
	ErrNetworkFull        = errors.New("network has no free addresses")
	ErrLastOwner          = errors.New("cannot remove or demote the last owner")
	ErrNoRelayAvailable   = errors.New("no relay servers available")
)
