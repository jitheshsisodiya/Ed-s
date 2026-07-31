// Package metrics exposes the relay node's Prometheus collectors.
//
// Metric names match the panels in deploy/monitoring/grafana/dashboards.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ActiveSessions gauges the relay sessions currently held by this node.
	ActiveSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_sessions",
		Help: "Number of relay sessions currently active on this node.",
	})

	// BoundDevices gauges individual peers attached across all sessions.
	BoundDevices = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "relay_bound_devices",
		Help: "Number of devices currently bound to this relay node.",
	})

	// BytesRelayedTotal counts bytes forwarded between peers.
	BytesRelayedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bytes_relayed_total",
		Help: "Total bytes forwarded between relayed peers.",
	})

	// PacketsRelayedTotal counts datagrams forwarded between peers.
	PacketsRelayedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_packets_relayed_total",
		Help: "Total datagrams forwarded between relayed peers.",
	})

	// PacketsDroppedTotal counts datagrams refused (unauthenticated,
	// unroutable or malformed).
	PacketsDroppedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "relay_packets_dropped_total",
		Help: "Total datagrams dropped without forwarding.",
	})
)

// Snapshot is the subset of relay.Stats the exporter publishes.
type Snapshot struct {
	Sessions       int
	Bindings       int
	BytesRelayed   uint64
	PacketsRelayed uint64
	PacketsDropped uint64
}

// Publish reconciles the collectors with a stats snapshot. The counters are
// monotonic totals held by the session table, so they are set by adding the
// delta since the previous publish rather than being overwritten.
type Publisher struct {
	lastBytes   uint64
	lastRelayed uint64
	lastDropped uint64
}

// Publish updates all collectors from a snapshot.
func (p *Publisher) Publish(s Snapshot) {
	ActiveSessions.Set(float64(s.Sessions))
	BoundDevices.Set(float64(s.Bindings))

	if s.BytesRelayed >= p.lastBytes {
		BytesRelayedTotal.Add(float64(s.BytesRelayed - p.lastBytes))
	}
	if s.PacketsRelayed >= p.lastRelayed {
		PacketsRelayedTotal.Add(float64(s.PacketsRelayed - p.lastRelayed))
	}
	if s.PacketsDropped >= p.lastDropped {
		PacketsDroppedTotal.Add(float64(s.PacketsDropped - p.lastDropped))
	}

	p.lastBytes = s.BytesRelayed
	p.lastRelayed = s.PacketsRelayed
	p.lastDropped = s.PacketsDropped
}

// Handler returns the Prometheus scrape handler.
func Handler() http.Handler { return promhttp.Handler() }
