package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/jitheshsisodiya/Ed-s/client/internal/apiclient"
	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

func cmdLogin(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("login")
	server := fs.String("server", "", "control plane URL (e.g. https://api.nexusvpn.example.com)")
	email := fs.String("email", "", "account email")
	password := fs.String("password", "", "account password (prompted if omitted)")
	mfaCode := fs.String("mfa", "", "TOTP code, if MFA is enabled")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := store.Load()
	if err != nil {
		return err
	}

	if *server != "" {
		cfg.ServerURL = strings.TrimRight(*server, "/")
	}
	if cfg.ServerURL == "" {
		return fmt.Errorf("no server configured - pass -server https://your-control-plane")
	}

	if *email == "" {
		v, err := prompt("Email: ")
		if err != nil {
			return err
		}
		*email = v
	}

	if *password == "" {
		fmt.Print("Password: ")
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return fmt.Errorf("read password: %w", err)
		}
		*password = string(secret)
	}

	client := apiclient.New(apiBaseURL(cfg.ServerURL), apiclient.Options{})
	tokens, err := client.Login(ctx, *email, *password, *mfaCode)
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}

	if tokens.MFARequired {
		// The account has MFA enabled and no code was supplied; ask for one
		// and retry rather than failing outright.
		code, err := prompt("MFA code: ")
		if err != nil {
			return err
		}
		tokens, err = client.Login(ctx, *email, *password, code)
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}
	}

	_, err = store.Update(func(c *config.Config) error {
		c.ServerURL = cfg.ServerURL
		c.AccessToken = tokens.AccessToken
		c.RefreshToken = tokens.RefreshToken
		c.UserEmail = *email
		return nil
	})
	if err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	fmt.Printf("Logged in to %s as %s\n", cfg.ServerURL, *email)
	return nil
}

func cmdLogout(ctx context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("logout")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	cfg, err := store.Load()
	if err != nil {
		return err
	}
	if !cfg.IsLoggedIn() {
		fmt.Println("Not logged in.")
		return nil
	}

	// Best effort: revoke the refresh token server-side. Local credentials
	// are cleared regardless, so a user can always sign out offline.
	client := newAPIClient(store, cfg)
	if err := client.Logout(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: server-side logout failed: %v\n", err)
	}

	if _, err := store.Update(func(c *config.Config) error {
		c.AccessToken = ""
		c.RefreshToken = ""
		c.UserEmail = ""
		return nil
	}); err != nil {
		return err
	}

	fmt.Println("Logged out.")
	return nil
}

func cmdDevice(ctx context.Context, store *config.Store, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexusvpnctl device rotate-key")
	}
	switch args[0] {
	case "rotate-key":
		return cmdDeviceRotateKey(ctx, store, args[1:])
	default:
		return fmt.Errorf("unknown device subcommand %q", args[0])
	}
}

func cmdDeviceRotateKey(_ context.Context, store *config.Store, args []string) error {
	fs := newFlagSet("device rotate-key")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	kp, err := config.GenerateKeypair()
	if err != nil {
		return fmt.Errorf("generate keypair: %w", err)
	}

	cfg, err := store.Update(func(c *config.Config) error {
		c.Keypair = kp
		// The old public key is registered against every joined network, so
		// those registrations are now stale. Clearing the cached device IDs
		// makes the next `up` re-register with the new key.
		for _, n := range c.Networks {
			n.DeviceID = ""
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Println("Generated a new device keypair.")
	fmt.Printf("  public key: %s\n", cfg.Keypair.PublicKey)
	fmt.Println("The next 'nexusvpnctl up' will re-register this device with the new key.")
	return nil
}
