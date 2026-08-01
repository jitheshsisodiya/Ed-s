package local

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
	natpmp "github.com/jackpal/go-nat-pmp"
)

// Port is one port to ask the router about.
//
// A list rather than a map keyed by protocol, because this needs to request
// two TCP ports — one for signing in, one for the tunnel — and a map would
// silently hold only the last of them.
type Port struct {
	Protocol string // "tcp" or "udp"
	Number   int
}

// Mapping is a port this machine has persuaded the router to forward.
type Mapping struct {
	// Internal is the port on this machine; External the one on the router,
	// which is usually but not always the same number.
	Internal int
	External int
	// Protocol is "tcp" or "udp".
	Protocol string
	// Method names what agreed to it, so the UI can say how this happened
	// and a person can find the corresponding entry in their router.
	Method string
}

// PortMapper asks the router to let the outside world in.
//
// Reaching a machine behind a home router from a phone on mobile data needs
// a hole in that router. The alternative is telling somebody to log into
// their router's admin page and add a forwarding rule, which is a different
// screen on every router, requires a password most people have never
// changed and cannot find, and is the point at which almost everybody stops.
//
// Both protocols are tried because router support is split and neither is
// universal: NAT-PMP and its successor PCP are common on Apple hardware and
// some others; UPnP is what most consumer routers ship. Many have one
// disabled. Some have both, and some have neither, which is a real answer
// this has to be able to give rather than hang on.
type PortMapper struct {
	logf func(string, ...any)

	mu sync.Mutex
	// requested accumulates every port ever asked for, because renewal has
	// to cover all of them. An earlier version replaced this on each call
	// and started a second renewal loop, so the ports from the first call
	// quietly stopped being renewed and the hole closed two hours later.
	requested []Port
	mappings  []Mapping
	external  string
	renewing  bool
	stop      chan struct{}
	stopped   bool
}

// NewPortMapper builds a PortMapper. logf may be nil.
func NewPortMapper(logf func(string, ...any)) *PortMapper {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &PortMapper{logf: logf, stop: make(chan struct{})}
}

// mapLifetime is how long the router is asked to hold a mapping.
//
// Deliberately short relative to how long the app runs. A router that is
// rebooted, or that quietly drops mappings under memory pressure — which
// cheap ones do — forgets everything, and a mapping requested once at
// startup would be gone with nothing to notice. Renewing means the hole
// reopens within the hour by itself. It also means a crashed app stops being
// forwarded to fairly soon, rather than leaving a machine exposed
// indefinitely.
const (
	mapLifetime = 2 * time.Hour
	renewEvery  = 30 * time.Minute
)

// Open asks the router to forward ports, and keeps them open until Close.
//
// Returns what was actually agreed, which may be nothing. An empty result is
// a normal outcome — plenty of networks have no router this can talk to, and
// carrier-grade NAT cannot be opened at all — so it is reported rather than
// treated as failure.
// Open may be called more than once; each call adds to what is kept open
// rather than replacing it, and a single renewal loop covers the lot.
func (p *PortMapper) Open(ctx context.Context, ports []Port) []Mapping {
	got := p.tryOnce(ctx, ports)

	p.mu.Lock()
	p.requested = append(p.requested, ports...)
	p.mappings = append(p.mappings, got...)
	start := !p.renewing && len(p.mappings) > 0
	if start {
		p.renewing = true
	}
	p.mu.Unlock()

	if start {
		go p.renew()
	}
	return got
}

// ExternalAddress is the address the outside world would use, or empty if
// this machine could not find out.
func (p *PortMapper) ExternalAddress() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.external
}

// Mappings returns what is currently open.
func (p *PortMapper) Mappings() []Mapping {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Mapping(nil), p.mappings...)
}

// Close stops renewing. The mappings are left to expire rather than deleted.
//
// Deleting them would be tidier and is the wrong trade: an app that is
// restarting — an upgrade, a crash, a reboot — would tear down its own hole
// and have to negotiate a new one, and some routers hand out a different
// external port each time. Letting a two-hour lease lapse closes it soon
// enough for a machine that is genuinely gone.
func (p *PortMapper) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.stopped = true
		close(p.stop)
	}
}

func (p *PortMapper) renew() {
	ticker := time.NewTicker(renewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.mu.Lock()
			ports := append([]Port(nil), p.requested...)
			p.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			got := p.tryOnce(ctx, ports)
			cancel()

			p.mu.Lock()
			had := len(p.mappings)
			p.mappings = got
			p.mu.Unlock()

			if len(got) == 0 && had > 0 {
				p.logf("local: the router has stopped forwarding; this machine " +
					"is reachable on the local network only")
			}
		}
	}
}

// tryOnce attempts both protocols, preferring whichever answers.
func (p *PortMapper) tryOnce(ctx context.Context, ports []Port) []Mapping {
	if got, external := p.viaNATPMP(ctx, ports); len(got) > 0 {
		p.setExternal(external)
		return got
	}
	if got, external := p.viaUPnP(ctx, ports); len(got) > 0 {
		p.setExternal(external)
		return got
	}
	return nil
}

func (p *PortMapper) setExternal(addr string) {
	if addr == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.external = addr
}

func (p *PortMapper) viaNATPMP(ctx context.Context, ports []Port) ([]Mapping, string) {
	gw, err := defaultGateway()
	if err != nil {
		return nil, ""
	}
	client := natpmp.NewClientWithTimeout(gw, 3*time.Second)

	ext, err := client.GetExternalAddress()
	if err != nil {
		return nil, ""
	}
	external := net.IPv4(ext.ExternalIPAddress[0], ext.ExternalIPAddress[1],
		ext.ExternalIPAddress[2], ext.ExternalIPAddress[3]).String()

	var out []Mapping
	for _, want := range ports {
		if ctx.Err() != nil {
			break
		}
		res, err := client.AddPortMapping(want.Protocol, want.Number, want.Number,
			int(mapLifetime.Seconds()))
		if err != nil {
			continue
		}
		out = append(out, Mapping{
			Internal: want.Number,
			External: int(res.MappedExternalPort),
			Protocol: want.Protocol,
			Method:   "NAT-PMP",
		})
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, external
}

func (p *PortMapper) viaUPnP(ctx context.Context, ports []Port) ([]Mapping, string) {
	clients, _, err := internetgateway2.NewWANIPConnection2Clients()
	if err != nil || len(clients) == 0 {
		return p.viaUPnPv1(ctx, ports)
	}
	client := clients[0]

	external, err := client.GetExternalIPAddress()
	if err != nil {
		return nil, ""
	}
	internal, err := internalAddressFor(client.Location.Hostname())
	if err != nil {
		return nil, ""
	}

	var out []Mapping
	for _, want := range ports {
		if ctx.Err() != nil {
			break
		}
		err := client.AddPortMapping("", uint16(want.Number), upnpProto(want.Protocol),
			uint16(want.Number), internal, true, "NexusVPN", uint32(mapLifetime.Seconds()))
		if err != nil {
			continue
		}
		out = append(out, Mapping{
			Internal: want.Number, External: want.Number,
			Protocol: want.Protocol, Method: "UPnP",
		})
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, external
}

// viaUPnPv1 covers routers that only speak the older profile, which is a lot
// of them — the version 2 service was introduced in 2010 and plenty of
// hardware in service predates it or never implemented it.
func (p *PortMapper) viaUPnPv1(ctx context.Context, ports []Port) ([]Mapping, string) {
	clients, _, err := internetgateway2.NewWANIPConnection1Clients()
	if err != nil || len(clients) == 0 {
		return nil, ""
	}
	client := clients[0]

	external, err := client.GetExternalIPAddress()
	if err != nil {
		return nil, ""
	}
	internal, err := internalAddressFor(client.Location.Hostname())
	if err != nil {
		return nil, ""
	}

	var out []Mapping
	for _, want := range ports {
		if ctx.Err() != nil {
			break
		}
		err := client.AddPortMapping("", uint16(want.Number), upnpProto(want.Protocol),
			uint16(want.Number), internal, true, "NexusVPN", uint32(mapLifetime.Seconds()))
		if err != nil {
			continue
		}
		out = append(out, Mapping{
			Internal: want.Number, External: want.Number,
			Protocol: want.Protocol, Method: "UPnP",
		})
	}
	if len(out) == 0 {
		return nil, ""
	}
	return out, external
}

func upnpProto(proto string) string {
	if proto == "udp" {
		return "UDP"
	}
	return "TCP"
}

// internalAddressFor returns the address this machine has on the same
// network as the router, which is what a forwarding rule has to name.
//
// Asked of the routing table rather than assumed, because a machine with
// several interfaces — which is any machine running a VPN — has several
// answers and only one of them is on the router's side.
func internalAddressFor(gateway string) (string, error) {
	conn, err := net.DialTimeout("udp4", net.JoinHostPort(gateway, "9"), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return "", fmt.Errorf("local: could not determine this machine's address on the router's network")
	}
	return addr.IP.String(), nil
}

// defaultGateway finds the router, which NAT-PMP requires as an explicit
// address rather than discovering for itself.
//
// Derived from the address the operating system says it would send from: the
// gateway is on that network, and .1 is where it sits on essentially every
// consumer router. A guess, and a cheap one — if it is wrong the NAT-PMP
// attempt times out in three seconds and UPnP, which discovers the router
// properly by multicast, is tried next.
func defaultGateway() (net.IP, error) {
	conn, err := net.DialTimeout("udp4", "192.0.2.1:9", 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return nil, fmt.Errorf("local: no route to the outside world")
	}
	v4 := addr.IP.To4()
	if v4 == nil {
		return nil, fmt.Errorf("local: no IPv4 route")
	}
	return net.IPv4(v4[0], v4[1], v4[2], 1), nil
}
