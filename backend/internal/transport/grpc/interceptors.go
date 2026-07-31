// Package grpc implements the CoordinationService gRPC server defined in
// proto/coordination/v1/coordination.proto. Clients authenticate with the
// same JWT access token used by the REST API, supplied in the "authorization"
// metadata key as "Bearer <token>".
package grpc

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/metrics"
)

type contextKey string

const ctxKeyUserID contextKey = "grpcUserID"

// TokenParser validates access tokens presented by gRPC clients.
type TokenParser interface {
	ParseAccessToken(tokenStr string) (*auth.AccessClaims, error)
}

// userIDFromContext returns the authenticated caller's user ID.
func userIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	return id, ok
}

// authenticate extracts and validates the bearer token from gRPC metadata.
func authenticate(ctx context.Context, tokens TokenParser) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
	}
	raw := strings.TrimSpace(strings.TrimPrefix(values[0], "Bearer "))
	if raw == "" {
		return nil, status.Error(codes.Unauthenticated, "empty bearer token")
	}
	claims, err := tokens.ParseAccessToken(raw)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
	}
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "malformed subject claim")
	}
	return context.WithValue(ctx, ctxKeyUserID, userID), nil
}

// AuthUnaryInterceptor authenticates every unary RPC.
func AuthUnaryInterceptor(tokens TokenParser) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		authedCtx, err := authenticate(ctx, tokens)
		if err != nil {
			return nil, err
		}
		return handler(authedCtx, req)
	}
}

// authedStream wraps a ServerStream so downstream handlers see the
// authenticated context.
type authedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authedStream) Context() context.Context { return s.ctx }

// AuthStreamInterceptor authenticates every streaming RPC.
func AuthStreamInterceptor(tokens TokenParser) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		authedCtx, err := authenticate(ss.Context(), tokens)
		if err != nil {
			return err
		}
		return handler(srv, &authedStream{ServerStream: ss, ctx: authedCtx})
	}
}

// MetricsUnaryInterceptor records Prometheus counters/histograms per RPC.
func MetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		code := status.Code(err)
		metrics.GRPCRequestsTotal.WithLabelValues(info.FullMethod, code.String()).Inc()
		metrics.GRPCRequestDuration.WithLabelValues(info.FullMethod).Observe(time.Since(start).Seconds())
		return resp, err
	}
}

// LoggingUnaryInterceptor logs each RPC outcome with structured fields.
func LoggingUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		fields := []zap.Field{
			zap.String("method", info.FullMethod),
			zap.String("code", status.Code(err).String()),
			zap.Duration("duration", time.Since(start)),
		}
		if err != nil {
			logger.Warn("grpc_request", append(fields, zap.Error(err))...)
		} else {
			logger.Info("grpc_request", fields...)
		}
		return resp, err
	}
}

// RecoveryUnaryInterceptor converts a panic into an Internal error instead of
// tearing down the server process.
func RecoveryUnaryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("grpc_panic_recovered", zap.Any("panic", rec), zap.String("method", info.FullMethod))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}
