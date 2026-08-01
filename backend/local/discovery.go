package local

import (
	"fmt"
	"os"
	"strings"

	"github.com/grandcat/zeroconf"
)

// ServiceType is how NexusVPN announces itself on a local network.
//
// The same mechanism a printer or a Chromecast uses: the machine shouts its
// name onto the network, and anything looking for one asks and gets a live
// answer.
const ServiceType = "_nexusvpn._tcp"

// Announcer publishes this control plane on the local network.
//
// It exists to stop addresses being written down. Every address embedded
// anywhere — typed into a phone, carried in a pairing code, remembered in a
// config file — is correct until the router hands this machine a different
// one, which it will, because addresses are leases. Then the pairing code
// points at nothing, the phone that paired last week cannot find the server,
// and none of it announces itself as an addressing problem: it just stops
// working.
//
// Asking the network "where is NexusVPN?" has no such expiry date.
type Announcer struct {
	server *zeroconf.Server
}

// Announce publishes the control plane, returning nil if the network will not
// carry it — which is normal on a guest network, on plenty of enterprise
// wireless, and anywhere multicast is filtered.
//
// A failure here is not worth reporting as an error. Discovery is the
// convenient path, not the only one: the address still works, and a pairing
// code still carries it.
func Announce(port int, fingerprint string, logf func(string, ...any)) *Announcer {
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// The fingerprint travels in the announcement so a device that finds this
	// server by name learns which certificate to expect at the same moment.
	// Without it, discovery would hand somebody an address and no way to tell
	// the right server from anything else that answered — which is worse than
	// no discovery, because it looks like it worked.
	text := []string{
		"v=1",
		"fp=" + fingerprint,
	}

	server, err := zeroconf.Register(instanceName(), ServiceType, "local.", port, text, nil)
	if err != nil {
		logf("local: not announcing on this network (%v); other machines can "+
			"still be pointed at the address directly", err)
		return nil
	}
	logf("local: announcing as %q, so other machines can find this one by name", instanceName())
	return &Announcer{server: server}
}

// Stop withdraws the announcement.
//
// Worth doing rather than leaving to time out: a name that still resolves to
// a machine that has stopped serving sends devices to a closed port, and they
// wait for a timeout instead of being told there is nothing there.
func (a *Announcer) Stop() {
	if a != nil && a.server != nil {
		a.server.Shutdown()
	}
}

// instanceName is what somebody sees when their phone lists what it found, so
// it is this machine's own name rather than a product name — on a network
// with three of these, "NexusVPN" three times is not a choice anybody can
// make.
func instanceName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "NexusVPN"
	}
	// Trailing dots and domain suffixes come back from some systems and read
	// badly in a list.
	host = strings.TrimSuffix(strings.TrimSuffix(host, "."), ".local")
	return fmt.Sprintf("NexusVPN on %s", host)
}
