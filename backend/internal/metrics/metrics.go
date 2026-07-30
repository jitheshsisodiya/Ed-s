// Package metrics defines the Prometheus metrics exposed by the backend on
// :9091/metrics.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// HTTPRequestsTotal counts REST requests by method, path pattern and status.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests processed.",
	}, []string{"method", "path", "status"})

	// HTTPRequestDuration observes REST request latency in seconds.
	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	// GRPCRequestsTotal counts gRPC requests by method and status code.
	GRPCRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_requests_total",
		Help: "Total number of gRPC requests processed.",
	}, []string{"method", "code"})

	// GRPCRequestDuration observes gRPC request latency in seconds.
	GRPCRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "grpc_request_duration_seconds",
		Help:    "gRPC request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method"})

	// ActiveWebSocketConnections is a gauge of currently open WS connections.
	ActiveWebSocketConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_websocket_connections",
		Help: "Number of currently open WebSocket connections.",
	})

	// DevicesOnline is a gauge of devices currently marked online in presence.
	DevicesOnline = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "devices_online",
		Help: "Number of devices currently online (presence set size).",
	})

	// ActiveGRPCStreams is a gauge of currently open StreamPeerUpdates streams.
	ActiveGRPCStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_grpc_peer_streams",
		Help: "Number of currently open StreamPeerUpdates gRPC streams.",
	})
)

// Handler returns the Prometheus scrape handler.
func Handler() http.Handler {
	return promhttp.Handler()
}
