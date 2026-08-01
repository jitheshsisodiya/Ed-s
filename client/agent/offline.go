package agent

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/protogen/coordination/v1"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

// rememberRegistration stores what the control plane said, so the tunnel can
// come up again without it.
//
// The peers, the address, the network — everything WireGuard needs was
// already known the last time this device connected. Requiring the control
// plane to repeat it is what turns "away from home" into "does not work",
// because reaching a machine behind somebody's router needs an inbound
// connection that router may simply refuse.
func (a *Agent) rememberRegistration(networkID string, resp *coordinationv1.RegisterDeviceResponse) {
	if resp == nil {
		return
	}
	blob, err := protojson.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = a.store.Update(func(c *config.Config) error {
		if c.Networks == nil {
			c.Networks = map[string]*config.NetworkState{}
		}
		st := c.Networks[networkID]
		if st == nil {
			st = &config.NetworkState{NetworkID: networkID}
			c.Networks[networkID] = st
		}
		st.Cached = &config.CachedRegistration{
			Response: string(blob),
			SavedAt:  time.Now(),
		}
		return nil
	})
}

// cachedRegistration returns the last known configuration for a network, and
// how old it is.
func (a *Agent) cachedRegistration(cfg *config.Config, networkID string) (*coordinationv1.RegisterDeviceResponse, time.Time, bool) {
	st := cfg.Networks[networkID]
	if st == nil || st.Cached == nil || st.Cached.Response == "" {
		return nil, time.Time{}, false
	}
	var resp coordinationv1.RegisterDeviceResponse
	if err := protojson.Unmarshal([]byte(st.Cached.Response), &resp); err != nil {
		return nil, time.Time{}, false
	}
	// An address is the one thing that cannot be worked around: without it
	// there is no interface to configure and nothing to fall back to.
	if resp.GetAssignedVirtualIp() == "" || resp.GetNetworkCidr() == "" {
		return nil, time.Time{}, false
	}
	return &resp, st.Cached.SavedAt, true
}

// controlPlaneUnreachable reports whether registration failed because nothing
// answered, as opposed to answering and saying no.
//
// The distinction is the whole safety of connecting from cache. A server that
// refused — because this device was removed from the network, or its key is
// no longer accepted — has given an answer, and honouring a cached one
// instead would be reconnecting a device that has been told to go away.
// Silence is different: it means nothing was asked, and what was true last
// time is still the best information available.
func controlPlaneUnreachable(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	case codes.Unknown:
		// A dial that never reached a gRPC server at all surfaces here.
		return true
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// describeCacheAge turns a timestamp into something worth putting in a log
// line, because connecting on old information is worth knowing about.
func describeCacheAge(saved time.Time) string {
	if saved.IsZero() {
		return "unknown age"
	}
	age := time.Since(saved)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%d minutes old", int(age.Minutes()))
	case age < 48*time.Hour:
		return fmt.Sprintf("%d hours old", int(age.Hours()))
	default:
		return fmt.Sprintf("%d days old", int(age.Hours()/24))
	}
}
