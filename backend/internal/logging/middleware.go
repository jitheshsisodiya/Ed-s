package logging

import (
	"net/http"
	"time"

	"go.uber.org/zap"
)

// statusRecorder captures the status code written by downstream handlers.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// HTTPMiddleware logs every request at info level (warn/error for 4xx/5xx).
func HTTPMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			dur := time.Since(start)

			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", rec.status),
				zap.Duration("duration", dur),
				zap.String("remote_addr", r.RemoteAddr),
			}
			switch {
			case rec.status >= 500:
				logger.Error("http_request", fields...)
			case rec.status >= 400:
				logger.Warn("http_request", fields...)
			default:
				logger.Info("http_request", fields...)
			}
		})
	}
}
