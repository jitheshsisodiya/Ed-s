// Package http implements the NexusVPN REST API described in
// api/openapi.yaml on top of chi, including JWT auth + RBAC middleware and
// an error envelope matching the OpenAPI Error schema.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// errorEnvelope matches components.schemas.Error in api/openapi.yaml.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var env errorEnvelope
	env.Error.Code = code
	env.Error.Message = message
	writeJSON(w, status, env)
}

// writeDomainError translates a domain sentinel error (or a plain error) to
// the appropriate HTTP status + error envelope.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		writeError(w, http.StatusConflict, "already_exists", err.Error())
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
	case errors.Is(err, domain.ErrInvalidMFACode):
		writeError(w, http.StatusUnauthorized, "invalid_mfa_code", err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized", err.Error())
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, domain.ErrLastOwner):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, domain.ErrInviteInvalid):
		writeError(w, http.StatusBadRequest, "invite_invalid", err.Error())
	case errors.Is(err, domain.ErrNetworkFull):
		writeError(w, http.StatusConflict, "network_full", err.Error())
	case errors.Is(err, domain.ErrTokenExpired):
		writeError(w, http.StatusUnauthorized, "token_expired", err.Error())
	case errors.Is(err, domain.ErrTokenRevoked):
		writeError(w, http.StatusUnauthorized, "token_revoked", err.Error())
	case errors.Is(err, domain.ErrTokenInvalid):
		writeError(w, http.StatusUnauthorized, "token_invalid", err.Error())
	case errors.Is(err, domain.ErrMFARequired):
		writeError(w, http.StatusUnauthorized, "mfa_required", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}

// maxRequestBody bounds how much of a request body will be read.
//
// Every request this API accepts is a small JSON object — the largest is a
// network create with a description — so a mebibyte is generous by three
// orders of magnitude. Without a bound, an unauthenticated POST carrying a
// gigabyte-long string field is a gigabyte of allocation per connection, and
// the endpoints that need this most are exactly the ones that run before any
// credential has been checked.
const maxRequestBody = 1 << 20

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return errors.New("empty body")
	}
	// MaxBytesReader also caps what the server will read off the wire, so an
	// oversized body is refused rather than drained.
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
