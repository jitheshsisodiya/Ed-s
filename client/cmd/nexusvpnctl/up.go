package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"text/tabwriter"
	"time"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
	"github.com/jitheshsisodiya/Ed-s/client/internal/coordination"
	"github.com/jitheshsisodiya/Ed-s/client/internal/tunnel"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"
)

// defaultSTUNServers are used when the config doesn't override them. Two
// independent servers are required to detect symmetric NAT.
var defaultSTUNServers = []string{"stun.l.google.com:19302", "stun1.l.google.com:19302"}

// grpcTarget derives the coordination gRPC address from the server URL.
func grpcTarget(serverURL, override string) (target string, insecure bool) {
	if override != "" {
		return override, false
	}
	// Default convention: same host, gRPC port 9090. Deployments that
	// terminate gRPC elsewhere should pass -grpc explicitly.
	host := serverURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(host) > len(prefix) && host[:len(prefix)] == prefix {
			insecure = prefix == "http://"
			host = host[len(prefix):]
			break
		}
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.JoinHostPort(host, "9090"), insecure
}

func cmdUp(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("up")
	networkID := fs.String("network", "", "network ID to connect (defaults to the only joined network)")
	iface := fs.String("interface", "nexus0", "tunnel interface name")
	listenPort := fs.Int("port", 0, "WireGuard UDP listen port (0 = ephemeral)")
	grpcAddr := fs.String("grpc", "", "coordination gRPC address (default: server host on port 9090)")
	allowInsecureTLS := fs.Bool("insecure-skip-verify", false,
		"skip gRPC TLS certificate verification (test deployments only; disables MITM protection)")
	verbose := fs.Bool("v", false, "verbose WireGuard logging")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, client, err := requireLogin(store)
	if err != nil {
		return err
	}

	target, err := resolveNetwork(cfg, *networkID)
	if err != nil {
		return err
	}

	// Creating a TUN device needs elevated privileges; failing here with a
	// clear message beats a confusing permission error from the kernel.
	if os.Geteuid() != 0 && runtime.GOOS != "windows" {
		return fmt.Errorf("creating a tunnel interface requires root - re-run with sudo")
	}
	if *allowInsecureTLS {
		fmt.Fprintln(os.Stderr,
			"warning: -insecure-skip-verify disables gRPC certificate verification; traffic to the control plane can be intercepted")
	}

	// --- WireGuard interface ---
	dev, err := wireguard.New(wireguard.InterfaceConfig{
		Name:             *iface,
		PrivateKeyBase64: cfg.Keypair.PrivateKey,
		ListenPort:       *listenPort,
		LogVerbose:       *verbose,
	})
	if err != nil {
		return fmt.Errorf("create tunnel interface: %w", err)
	}
	defer dev.Close()

	port, err := dev.ListenPort()
	if err != nil {
		return fmt.Errorf("read listen port: %w", err)
	}
	fmt.Printf("Interface %s up on UDP port %d\n", dev.Name(), port)

	// --- Control plane ---
	gt, insecureGRPC := grpcTarget(cfg.ServerURL, *grpcAddr)
	coord, err := coordination.Dial(ctx, gt, client, coordination.DialOptions{
		Insecure:           insecureGRPC,
		InsecureSkipVerify: *allowInsecureTLS,
	})
	if err != nil {
		return fmt.Errorf("connect to coordination service: %w", err)
	}
	defer coord.Close()

	// --- STUN discovery socket ---
	// Discovery runs on its own socket. Sharing WireGuard's socket would
	// give a more faithful NAT mapping, but wireguard-go owns that socket
	// exclusively; the tunnel therefore treats a symmetric-NAT result as a
	// signal to fall back to relay rather than as a precise mapping.
	stunConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return fmt.Errorf("open STUN socket: %w", err)
	}
	defer stunConn.Close()

	stunServers := cfg.STUNServers
	if len(stunServers) < 2 {
		stunServers = defaultSTUNServers
	}

	tun, err := tunnel.New(tunnel.Options{
		Device:      dev,
		Coordinator: coordAdapter{coord},
		Discoverer: tunnel.STUNDiscoverer{
			Conn:            stunConn,
			PrimaryServer:   stunServers[0],
			SecondaryServer: stunServers[1],
		},
		Prober:        holePunchProber(stunConn),
		NetworkID:     target.NetworkID,
		DeviceName:    cfg.DeviceName,
		OS:            deviceOS(),
		OSVersion:     runtime.GOARCH,
		ClientVersion: Version,
		PublicKey:     cfg.Keypair.PublicKey,
		Logf:          func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
	})
	if err != nil {
		return err
	}
	tun.SetNetworkName(target.Name)

	// Register first so the interface can be given its assigned address
	// before any traffic is attempted.
	resp, err := tun.Register(ctx)
	if err != nil {
		return err
	}

	virtualIP := net.ParseIP(resp.GetAssignedVirtualIp())
	if virtualIP == nil {
		return fmt.Errorf("control plane assigned a malformed address %q", resp.GetAssignedVirtualIp())
	}
	_, netCIDR, err := net.ParseCIDR(resp.GetNetworkCidr())
	if err != nil {
		return fmt.Errorf("control plane returned a malformed CIDR %q: %w", resp.GetNetworkCidr(), err)
	}
	if err := dev.AssignAddress(virtualIP, netCIDR); err != nil {
		return fmt.Errorf("assign %s to %s: %w", virtualIP, dev.Name(), err)
	}
	// Route the network's address space into the tunnel so peers are
	// reachable by their virtual IPs.
	if err := dev.AddRoute(netCIDR); err != nil {
		return fmt.Errorf("route %s via %s: %w", netCIDR, dev.Name(), err)
	}
	fmt.Printf("Assigned %s on %s\n", virtualIP, netCIDR)

	// Persist what we learned so `status` works without a running tunnel.
	if _, err := store.Update(func(c *config.Config) error {
		state, ok := c.Networks[target.NetworkID]
		if !ok {
			state = &config.NetworkState{NetworkID: target.NetworkID, JoinedAt: time.Now()}
			c.Networks[target.NetworkID] = state
		}
		state.Name = target.Name
		state.DeviceID = resp.GetDeviceId()
		state.VirtualIP = resp.GetAssignedVirtualIp()
		state.CIDR = resp.GetNetworkCidr()
		state.DNSServers = resp.GetDnsServers()
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not persist network state: %v\n", err)
	}

	fmt.Printf("Connected to %q. Press Ctrl-C to disconnect.\n\n", target.Name)

	// Start blocks until ctx is cancelled (Ctrl-C / SIGTERM).
	if err := tun.Start(ctx); err != nil && ctx.Err() == nil {
		return err
	}

	fmt.Println("\nDisconnected.")
	return nil
}

// coordAdapter adapts *coordination.Client to tunnel.Coordinator. Only
// StreamPeerUpdates needs adapting: the two packages name the stream
// interface separately, and the rest of the methods match exactly.
type coordAdapter struct{ *coordination.Client }

func (a coordAdapter) StreamPeerUpdates(ctx context.Context, deviceID string) (tunnel.PeerUpdateStream, error) {
	return a.Client.StreamPeerUpdates(ctx, deviceID)
}

// resolveNetwork picks the network to connect: the one requested, or the
// only joined network if there is exactly one.
func resolveNetwork(cfg *config.Config, networkID string) (*config.NetworkState, error) {
	if networkID != "" {
		state, ok := cfg.Network(networkID)
		if !ok {
			// Not joined locally yet, but the ID may still be valid
			// server-side; let the control plane be the authority.
			return &config.NetworkState{NetworkID: networkID}, nil
		}
		return state, nil
	}

	switch len(cfg.Networks) {
	case 0:
		return nil, fmt.Errorf("no networks joined - run 'nexusvpnctl network join -code <invite code>'")
	case 1:
		for _, state := range cfg.Networks {
			return state, nil
		}
	}
	return nil, fmt.Errorf("multiple networks joined - pass -network <id> (see 'nexusvpnctl network list')")
}

// deviceOS reports this device's OS using the control plane's vocabulary.
func deviceOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return "unknown"
	}
}

// holePunchProber sends probe datagrams from the discovery socket.
func holePunchProber(conn *net.UDPConn) func(*net.UDPAddr) error {
	return func(addr *net.UDPAddr) error {
		// A single zero byte is enough to open the NAT pinhole; it is not a
		// valid WireGuard packet, so the peer ignores it harmlessly.
		_, err := conn.WriteToUDP([]byte{0}, addr)
		return err
	}
}

func cmdDown(store *config.Store, args []string) error {
	fs := newFlagSet("down")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	// `up` runs in the foreground and owns the tunnel interface for its
	// lifetime, so stopping it is what tears the tunnel down. Keeping the
	// lifecycle tied to one process avoids a background daemon holding
	// elevated privileges and a private key in memory indefinitely.
	fmt.Println("The tunnel is owned by the running 'nexusvpnctl up' process.")
	fmt.Println("Stop it with Ctrl-C in that terminal (or send it SIGTERM) to disconnect.")
	return nil
}

func cmdStatus(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("status")
	networkID := fs.String("network", "", "network ID (defaults to the only joined network)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, client, err := requireLogin(store)
	if err != nil {
		return err
	}

	target, err := resolveNetwork(cfg, *networkID)
	if err != nil {
		return err
	}

	fmt.Printf("Server:   %s\n", cfg.ServerURL)
	fmt.Printf("Account:  %s\n", cfg.UserEmail)
	fmt.Printf("Network:  %s (%s)\n", target.Name, target.NetworkID)
	if target.VirtualIP != "" {
		fmt.Printf("This device: %s on %s\n", target.VirtualIP, target.CIDR)
	}
	fmt.Println()

	devices, err := client.ListDevices(ctx, target.NetworkID)
	if err != nil {
		return fmt.Errorf("list devices: %w", err)
	}
	if len(devices) == 0 {
		fmt.Println("No devices registered on this network yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DEVICE\tVIRTUAL IP\tOS\tSTATUS\tNAT\tLAST SEEN")
	for _, d := range devices {
		marker := ""
		if d.ID == target.DeviceID {
			marker = " (this device)"
		}
		natType := d.NATType
		if natType == "" {
			natType = "-"
		}
		lastSeen := d.LastSeenAt
		if lastSeen == "" {
			lastSeen = "-"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name, marker, d.VirtualIP, d.OS, d.Status, natType, lastSeen)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Println("\nPer-peer connection mode (direct or relayed) is reported live by")
	fmt.Println("the running 'nexusvpnctl up' process.")
	return nil
}
