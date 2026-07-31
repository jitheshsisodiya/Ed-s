package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jitheshsisodiya/Ed-s/client/internal/apiclient"
	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

// newFlagSet builds a FlagSet that reports -h as flagErrHelp rather than
// exiting the process, so main can distinguish help from failure.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

// parseFlags parses args, translating flag.ErrHelp into flagErrHelp.
func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flagErrHelp
		}
		return err
	}
	return nil
}

// apiBaseURL derives the REST base URL from a server root, tolerating a
// value that already includes the /api/v1 suffix.
func apiBaseURL(serverURL string) string {
	trimmed := strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(trimmed, "/api/v1") {
		return trimmed
	}
	return trimmed + "/api/v1"
}

// newAPIClient builds a REST client from stored config, wiring token refresh
// back into the config file so a refreshed session survives restarts.
func newAPIClient(store *config.Store, cfg *config.Config) *apiclient.Client {
	client := apiclient.New(apiBaseURL(cfg.ServerURL), apiclient.Options{
		OnTokenRefresh: func(tp apiclient.TokenPair) {
			// Persist rotated tokens; a failure here is not fatal to the
			// in-flight request, so it is only reported.
			if _, err := store.Update(func(c *config.Config) error {
				c.AccessToken = tp.AccessToken
				c.RefreshToken = tp.RefreshToken
				return nil
			}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not persist refreshed tokens: %v\n", err)
			}
		},
	})
	client.SetTokens(cfg.AccessToken, cfg.RefreshToken)
	return client
}

// requireLogin loads config and fails with a helpful message if the user
// has not authenticated yet.
func requireLogin(store *config.Store) (*config.Config, *apiclient.Client, error) {
	cfg, err := store.Load()
	if err != nil {
		return nil, nil, err
	}
	if !cfg.IsLoggedIn() {
		return nil, nil, errors.New("not logged in - run 'nexusvpnctl login' first")
	}
	return cfg, newAPIClient(store, cfg), nil
}

// prompt reads a single line from stdin after printing a label.
func prompt(label string) (string, error) {
	fmt.Print(label)
	var line string
	if _, err := fmt.Scanln(&line); err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
