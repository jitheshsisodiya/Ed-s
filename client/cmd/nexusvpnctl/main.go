// Command nexusvpnctl is the NexusVPN client agent: it authenticates against
// a control plane, joins virtual networks, and brings up an encrypted
// WireGuard tunnel that reaches peers directly when NAT allows and via a
// relay when it doesn't.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

// Version is the client version reported to the control plane. Release
// builds override it with -ldflags "-X main.Version=v1.2.3".
var Version = "dev"

const usage = `nexusvpnctl - NexusVPN client

Usage:
  nexusvpnctl <command> [flags]

Commands:
  login              Authenticate against a control plane
  logout             Sign out and clear stored tokens
  network list       List the networks this account belongs to
  network create     Create a new network
  network join       Join a network with an invite code
  network leave      Forget a network locally
  up                 Bring up the tunnel (requires root/Administrator)
  down               Stop a running tunnel
  status             Show tunnel and peer status
  device rotate-key  Generate a new device keypair
  version            Print the client version

Run 'nexusvpnctl <command> -h' for command-specific flags.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flagErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// flagErrHelp signals that -h was handled and the process should exit 0.
var flagErrHelp = errors.New("help requested")

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	// A single root context cancelled on Ctrl-C, so long-running commands
	// (notably `up`) shut down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := config.NewStore()
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}

	switch args[0] {
	case "login":
		return cmdLogin(ctx, store, args[1:])
	case "logout":
		return cmdLogout(ctx, store, args[1:])
	case "network":
		return cmdNetwork(ctx, store, args[1:])
	case "device":
		return cmdDevice(ctx, store, args[1:])
	case "up":
		return cmdUp(ctx, store, args[1:])
	case "down":
		return cmdDown(store, args[1:])
	case "status":
		return cmdStatus(ctx, store, args[1:])
	case "version":
		fmt.Printf("nexusvpnctl %s\n", Version)
		return nil
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}
