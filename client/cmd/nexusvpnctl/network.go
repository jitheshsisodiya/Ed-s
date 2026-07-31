package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

func cmdNetwork(ctx context.Context, store *config.Store, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexusvpnctl network <list|create|join|leave>")
	}
	switch args[0] {
	case "list":
		return cmdNetworkList(ctx, store, args[1:])
	case "create":
		return cmdNetworkCreate(ctx, store, args[1:])
	case "join":
		return cmdNetworkJoin(ctx, store, args[1:])
	case "leave":
		return cmdNetworkLeave(store, args[1:])
	default:
		return fmt.Errorf("unknown network subcommand %q", args[0])
	}
}

func cmdNetworkList(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("network list")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, client, err := requireLogin(store)
	if err != nil {
		return err
	}

	networks, err := client.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	if len(networks) == 0 {
		fmt.Println("No networks yet. Create one with 'nexusvpnctl network create -name <name>'")
		fmt.Println("or join an existing one with 'nexusvpnctl network join -code <invite code>'.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tCIDR\tROLE\tMEMBERS\tDEVICES\tLOCAL\tID")
	for _, n := range networks {
		local := "-"
		if state, ok := cfg.Network(n.ID); ok {
			local = state.VirtualIP
			if local == "" {
				local = "joined"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			n.Name, n.CIDR, n.Role, n.MemberCount, n.DeviceCount, local, n.ID)
	}
	return w.Flush()
}

func cmdNetworkCreate(ctx context.Context, store *config.Store, args []string) error {
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

	_, client, err := requireLogin(store)
	if err != nil {
		return err
	}

	network, err := client.CreateNetwork(ctx, *name, *description, *cidr, nil)
	if err != nil {
		return fmt.Errorf("create network: %w", err)
	}

	if _, err := store.Update(func(c *config.Config) error {
		c.Networks[network.ID] = &config.NetworkState{
			NetworkID: network.ID,
			Name:      network.Name,
			CIDR:      network.CIDR,
			JoinedAt:  time.Now(),
		}
		return nil
	}); err != nil {
		return err
	}

	fmt.Printf("Created network %q (%s)\n", network.Name, network.CIDR)
	fmt.Printf("  ID:          %s\n", network.ID)
	fmt.Printf("  Invite code: %s\n", network.InviteCode)
	fmt.Println("\nShare the invite code so others can run:")
	fmt.Printf("  nexusvpnctl network join -code %s\n", network.InviteCode)
	return nil
}

func cmdNetworkJoin(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("network join")
	code := fs.String("code", "", "invite code (required)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *code == "" {
		return fmt.Errorf("-code is required")
	}

	_, client, err := requireLogin(store)
	if err != nil {
		return err
	}

	network, err := client.JoinNetwork(ctx, *code)
	if err != nil {
		return fmt.Errorf("join network: %w", err)
	}

	if _, err := store.Update(func(c *config.Config) error {
		if existing, ok := c.Networks[network.ID]; ok {
			existing.Name = network.Name
			existing.CIDR = network.CIDR
			return nil
		}
		c.Networks[network.ID] = &config.NetworkState{
			NetworkID: network.ID,
			Name:      network.Name,
			CIDR:      network.CIDR,
			JoinedAt:  time.Now(),
		}
		return nil
	}); err != nil {
		return err
	}

	fmt.Printf("Joined network %q (%s)\n", network.Name, network.CIDR)
	fmt.Println("Bring up the tunnel with:")
	fmt.Printf("  sudo nexusvpnctl up -network %s\n", network.ID)
	return nil
}

func cmdNetworkLeave(store *config.Store, args []string) error {
	fs := newFlagSet("network leave")
	networkID := fs.String("network", "", "network ID to forget (required)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *networkID == "" {
		return fmt.Errorf("-network is required")
	}

	cfg, err := store.Update(func(c *config.Config) error {
		if _, ok := c.Networks[*networkID]; !ok {
			return fmt.Errorf("network %s is not joined locally", *networkID)
		}
		delete(c.Networks, *networkID)
		return nil
	})
	if err != nil {
		return err
	}
	_ = cfg

	// This only forgets the network on this device. Membership and the
	// registered device remain server-side until removed by an admin or via
	// the admin panel, which is deliberate: `leave` must not be able to
	// silently orphan a device record other members can still see.
	fmt.Printf("Forgot network %s locally.\n", *networkID)
	fmt.Println("Note: your membership and device registration still exist on the server.")
	return nil
}
