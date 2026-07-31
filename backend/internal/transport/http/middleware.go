package http

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/metrics"
)

type contextKey string

const (
	ctxKeyUserID contextKey = "userID"
	ctxKeyEmail  contextKey = "email"
)

// TokenParser is the subset of auth.TokenManager the middleware needs.
type TokenParser interface {
	ParseAccessToken(tokenStr string) (*auth.AccessClaims, error)
}

// UserIDFromContext returns the authenticated user's ID, if any.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	return id, ok
}

// EmailFromContext returns the authenticated user's email, if any.
func EmailFromContext(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(ctxKeyEmail).(string)
	return email, ok
}

// WithUserID returns a copy of ctx carrying an authenticated user identity.
// Exported so other transports (e.g. the WebSocket hub) can reuse it.
func WithUserID(ctx context.Context, userID uuid.UUID, email string) context.Context {
	ctx = context.WithValue(ctx, ctxKeyUserID, userID)
	return context.WithValue(ctx, ctxKeyEmail, email)
}

// bearerToken extracts a token from the Authorization header, falling back to
// an `access_token` query parameter (used by WebSocket clients, which cannot
// set custom headers from a browser).
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return r.URL.Query().Get("access_token")
}

// Authenticate rejects requests without a valid access token and injects the
// caller's identity into the request context.
func Authenticate(tokens TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := bearerToken(r)
			if raw == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			claims, err := tokens.ParseAccessToken(raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "token_invalid", "invalid or expired access token")
				return
			}
			userID, err := uuid.Parse(claims.UserID)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "token_invalid", "malformed subject claim")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), userID, claims.Email)))
		})
	}
}

// CORS applies permissive-but-configurable cross-origin headers so the admin
// panel (served from a different origin in development) can call the API.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
		}
		allowed[o] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				_, ok := allowed[origin]
				if allowAll || ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					w.Header().Set("Access-Control-Max-Age", "600")
				}
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// metricsRecorder wraps ResponseWriter to capture the status code.
type metricsRecorder struct {
	http.ResponseWriter
	status int
}

func (m *metricsRecorder) WriteHeader(code int) {
	m.status = code
	m.ResponseWriter.WriteHeader(code)
}

// Metrics records Prometheus counters/histograms for every request. The
// route pattern (not the raw path) is used as a label to keep cardinality
// bounded — Go 1.22+ exposes it via (*http.Request).Pattern.
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &metricsRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			pattern := r.Pattern
			if pattern == "" {
				pattern = "unmatched"
			}
			metrics.HTTPRequestsTotal.WithLabelValues(r.Method, pattern, strconv.Itoa(rec.status)).Inc()
			metrics.HTTPRequestDuration.WithLabelValues(r.Method, pattern).Observe(time.Since(start).Seconds())
		})
	}
}

// Recover converts a panic in any handler into a 500 error envelope rather
// than tearing down the whole server.
func Recover(onPanic func(any)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if onPanic != nil {
						onPanic(rec)
					}
					writeError(w, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// chain applies middleware in order, so chain(h, a, b) runs a -> b -> h.
func chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// clientIP extracts the best-guess client IP, honoring X-Forwarded-For when
// the service runs behind an ingress/load balancer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, found := strings.Cut(xff, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
