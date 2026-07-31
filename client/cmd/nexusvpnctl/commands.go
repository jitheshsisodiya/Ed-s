package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/jitheshsisodiya/Ed-s/client/agent"
)

// newFlagSet returns a FlagSet that surfaces -h as flagErrHelp instead of
// exiting, so main can distinguish help from failure.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flagErrHelp
		}
		return err
	}
	return nil
}

// prompt reads one line from stdin after printing a label.
func prompt(label string) (string, error) {
	fmt.Print(label)
	var line string
	if _, err := fmt.Scanln(&line); err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a line without echoing it.
func promptSecret(label string) (string, error) {
	fmt.Print(label)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(secret), nil
}

// --- auth ---

func cmdLogin(ctx context.Context, ag *agent.Agent, args []string) error {
	fs := newFlagSet("login")
	server := fs.String("server", "", "control plane URL (e.g. https://api.nexusvpn.example.com)")
	email := fs.String("email", "", "account email")
	password := fs.String("password", "", "account password (prompted if omitted)")
	mfaCode := fs.String("mfa", "", "TOTP code, if MFA is enabled")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	session, err := ag.Session()
	if err != nil {
		return err
	}
	serverURL := session.ServerURL
	if *server != "" {
		serverURL = *server
	}
	if serverURL == "" {
		return fmt.Errorf("no server configured - pass -server https://your-control-plane")
	}

	if *email == "" {
		if *email, err = prompt("Email: "); err != nil {
			return err
		}
	}
	if *password == "" {
		// Prompting keeps the password out of shell history and the process
		// table, unlike -password.
		if *password, err = promptSecret("Password: "); err != nil {
			return err
		}
	}

	err = ag.Login(ctx, serverURL, *email, *password, *mfaCode)
	if errors.Is(err, agent.ErrMFARequired) {
		code, promptErr := prompt("MFA code: ")
		if promptErr != nil {
			return promptErr
		}
		err = ag.Login(ctx, serverURL, *email, *password, code)
	}
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	fmt.Printf("Logged in to %s as %s\n", serverURL, *email)
	return nil
}

func cmdLogout(ctx context.Context, ag *agent.Agent, args []string) error {
	if err := parseFlags(newFlagSet("logout"), args); err != nil {
		return err
	}
	session, err := ag.Session()
	if err != nil {
		return err
	}
	if !session.LoggedIn {
		fmt.Println("Not logged in.")
		return nil
	}
	if err := ag.Logout(ctx); err != nil {
		return err
	}
	fmt.Println("Logged out.")
	return nil
}

// --- networks ---

func cmdNetwork(ctx context.Context, ag *agent.Agent, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexusvpnctl network <list|create|join|leave>")
	}
	switch args[0] {
	case "list":
		return cmdNetworkList(ctx, ag, args[1:])
	case "create":
		return cmdNetworkCreate(ctx, ag, args[1:])
	case "join":
		return cmdNetworkJoin(ctx, ag, args[1:])
	case "leave":
		return cmdNetworkLeave(ag, args[1:])
	default:
		return fmt.Errorf("unknown network subcommand %q", args[0])
	}
}

func cmdNetworkList(ctx context.Context, ag *agent.Agent, args []string) error {
	if err := parseFlags(newFlagSet("network list"), args); err != nil {
		return err
	}
	networks, err := ag.ListNetworks(ctx)
	if err != nil {
		return err
	}
	if len(networks) == 0 {
		fmt.Println("No networks yet. Create one with 'nexusvpnctl network create -name <name>'")
		fmt.Println("or join an existing one with 'nexusvpnctl network join -code <invite code>'.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCIDR\tROLE\tMEMBERS\tDEVICES\tJOINED\tID")
	for _, n := range networks {
		joined := "no"
		if n.Joined {
			joined = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			n.Name, n.CIDR, n.Role, n.MemberCount, n.DeviceCount, joined, n.ID)
	}
	return w.Flush()
}

func cmdNetworkCreate(ctx context.Context, ag *agent.Agent, args []string) error {
	fs := newFlagSet("network create")
	name := fs.String("name", "", "network name (required)")
	description := fs.String("description", "", "network description")
	cidr := fs.String("cidr", "10.77.0.0/24", "address space for the network")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *name == "" {
		return fmt.Errorf("-name is required")
	}

	n, err := ag.CreateNetwork(ctx, *name, *description, *cidr)
	if err != nil {
		return err
	}
	fmt.Printf("Created network %q (%s)\n", n.Name, n.CIDR)
	fmt.Printf("  ID:          %s\n", n.ID)
	fmt.Printf("  Invite code: %s\n", n.InviteCode)
	fmt.Println("\nShare the invite code so others can run:")
	fmt.Printf("  nexusvpnctl network join -code %s\n", n.InviteCode)
	return nil
}

func cmdNetworkJoin(ctx context.Context, ag *agent.Agent, args []string) error {
	fs := newFlagSet("network join")
	code := fs.String("code", "", "invite code (required)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("-code is required")
	}

	n, err := ag.JoinNetwork(ctx, *code)
	if err != nil {
		return err
	}
	fmt.Printf("Joined network %q (%s)\n", n.Name, n.CIDR)
	fmt.Println("Bring up the tunnel with:")
	fmt.Printf("  sudo nexusvpnctl up -network %s\n", n.ID)
	return nil
}

func cmdNetworkLeave(ag *agent.Agent, args []string) error {
	fs := newFlagSet("network leave")
	networkID := fs.String("network", "", "network ID to forget (required)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *networkID == "" {
		return fmt.Errorf("-network is required")
	}
	if err := ag.ForgetNetwork(*networkID); err != nil {
		return err
	}
	fmt.Printf("Forgot network %s locally.\n", *networkID)
	fmt.Println("Note: your membership and device registration still exist on the server.")
	return nil
}

// --- device ---

func cmdDevice(ag *agent.Agent, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexusvpnctl device <rotate-key|rename>")
	}
	switch args[0] {
	case "rotate-key":
		if err := parseFlags(newFlagSet("device rotate-key"), args[1:]); err != nil {
			return err
		}
		publicKey, err := ag.RotateDeviceKey()
		if err != nil {
			return err
		}
		fmt.Println("Generated a new device keypair.")
		fmt.Printf("  public key: %s\n", publicKey)
		fmt.Println("The next 'nexusvpnctl up' will re-register this device with the new key.")
		return nil

	case "rename":
		fs := newFlagSet("device rename")
		name := fs.String("name", "", "new device name (required)")
		if err := parseFlags(fs, args[1:]); err != nil {
			return err
		}
		if err := ag.SetDeviceName(*name); err != nil {
			return err
		}
		fmt.Printf("Device renamed to %q.\n", *name)
		return nil

	default:
		return fmt.Errorf("unknown device subcommand %q", args[0])
	}
}

// --- tunnel ---

func cmdUp(ctx context.Context, ag *agent.Agent, args []string) error {
	fs := newFlagSet("up")
	networkID := fs.String("network", "", "network ID (defaults to the only joined network)")
	iface := fs.String("interface", "nexus0", "tunnel interface name")
	listenPort := fs.Int("port", 0, "WireGuard UDP listen port (0 = ephemeral)")
	grpcAddr := fs.String("grpc", "", "coordination gRPC address (default: server host on port 9090)")
	insecureTLS := fs.Bool("insecure-skip-verify", false,
		"skip gRPC TLS certificate verification (test deployments only; disables protection against interception)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	id, name, err := ag.ResolveNetwork(*networkID)
	if err != nil {
		return fmt.Errorf("%w - see 'nexusvpnctl network list'", err)
	}
	if *insecureTLS {
		fmt.Fprintln(os.Stderr,
			"warning: -insecure-skip-verify disables certificate verification; traffic to the control plane can be intercepted")
	}

	err = ag.Connect(id, agent.ConnectOptions{
		InterfaceName:      *iface,
		ListenPort:         *listenPort,
		GRPCAddr:           *grpcAddr,
		InsecureSkipVerify: *insecureTLS,
		ClientVersion:      Version,
		Logf:               func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
	})
	if err != nil {
		return err
	}
	defer ag.Disconnect()

	label := name
	if label == "" {
		label = id
	}
	fmt.Printf("\nConnected to %s. Press Ctrl-C to disconnect.\n\n", label)

	// Block until the tunnel stops or the user interrupts.
	stopped := make(chan struct{})
	go func() { ag.Wait(); close(stopped) }()
	select {
	case <-ctx.Done():
	case <-stopped:
	}

	fmt.Println("\nDisconnected.")
	return nil
}

func cmdDown(args []string) error {
	if err := parseFlags(newFlagSet("down"), args); err != nil {
		return err
	}
	// `up` runs in the foreground and owns the tunnel for its lifetime, so
	// stopping that process is what tears the tunnel down. Keeping the
	// lifecycle tied to one process avoids a background daemon holding
	// elevated privileges and a private key in memory indefinitely.
	fmt.Println("The tunnel is owned by the running 'nexusvpnctl up' process.")
	fmt.Println("Stop it with Ctrl-C in that terminal (or send it SIGTERM) to disconnect.")
	return nil
}

func cmdStatus(ctx context.Context, ag *agent.Agent, args []string) error {
	fs := newFlagSet("status")
	networkID := fs.String("network", "", "network ID (defaults to the only joined network)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	session, err := ag.Session()
	if err != nil {
		return err
	}
	if !session.LoggedIn {
		return fmt.Errorf("not logged in - run 'nexusvpnctl login' first")
	}

	id, name, err := ag.ResolveNetwork(*networkID)
	if err != nil {
		return err
	}

	fmt.Printf("Server:   %s\n", session.ServerURL)
	fmt.Printf("Account:  %s\n", session.Email)
	fmt.Printf("Device:   %s\n", session.DeviceName)
	if name != "" {
		fmt.Printf("Network:  %s (%s)\n", name, id)
	} else {
		fmt.Printf("Network:  %s\n", id)
	}
	fmt.Println()

	devices, err := ag.Devices(ctx, id)
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("No devices registered on this network yet.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DEVICE\tVIRTUAL IP\tOS\tSTATUS\tNAT\tLAST SEEN")
	for _, d := range devices {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			d.Name, d.VirtualIP, d.OS, d.Status, orDash(d.NATType), orDash(d.LastSeenAt))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	fmt.Println("\nPer-peer connection mode (direct or relayed) is reported live by")
	fmt.Println("the running 'nexusvpnctl up' process.")
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
