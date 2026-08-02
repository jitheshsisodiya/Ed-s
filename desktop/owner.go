package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The account belonging to the machine that hosts the network.
//
// There is an account because the control plane listens on the network — on
// the LAN, and on the internet if a port was opened — and something has to
// stand between that socket and "add yourself to my network". There is no
// password prompt because a person typing one adds nothing: the tokens and
// the WireGuard private key already sit in this directory, readable by
// whoever is signed into this computer. A prompt would defend the front door
// of a house whose back door is open, at the cost of making everybody invent
// and remember a credential for their own PC.
//
// So the machine makes its own, and makes it far better than a person would:
// thirty-two bytes from crypto/rand, never typed, never reused, nothing to
// phish and nothing to guess. It is written down rather than derived, because
// a credential nobody can recover is a network nobody can get back into.
type owner struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// ownerFile sits beside the rest of this installation's data, under the same
// per-user directory and the same permissions as the tokens it exists to
// obtain. Somewhere stricter would be security theatre; somewhere looser
// would be a real downgrade.
func ownerPath() string { return filepath.Join(dataDir(), "owner.json") }

// newOwner mints credentials for this machine.
func newOwner(hostname string) (owner, error) {
	secret, err := randomSecret()
	if err != nil {
		return owner{}, err
	}
	name := machineLabel(hostname)
	return owner{
		// A local address on a reserved TLD, so it is obvious at a glance
		// that this is a machine account and that no mail was ever meant to
		// reach it.
		Email:    "owner@" + name + ".nexusvpn.local",
		Password: secret,
		Name:     name,
	}, nil
}

// randomSecret returns a password no human will ever see or type.
func randomSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("could not generate a secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// machineLabel reduces a hostname to something usable in an address.
//
// Hostnames carry spaces, underscores, accents and, on Windows, capitals.
// Anything left unrecognised is dropped rather than escaped, and a name that
// survives as nothing falls back to a fixed label — an address that fails to
// parse would turn "this machine hosts the network" into an error about
// email.
func machineLabel(hostname string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(hostname)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' && b.Len() > 0:
			b.WriteRune('-')
		}
	}
	label := strings.Trim(b.String(), "-")
	if label == "" {
		return "this-machine"
	}
	// Long enough for any real hostname, short enough to stay a label.
	if len(label) > 40 {
		label = strings.Trim(label[:40], "-")
	}
	return label
}

// loadOwner reads the stored credentials. Absent and unreadable are the same
// answer: there is nothing to sign in with, so ask.
func loadOwner() (owner, bool) {
	blob, err := os.ReadFile(ownerPath())
	if err != nil {
		return owner{}, false
	}
	var o owner
	if err := json.Unmarshal(blob, &o); err != nil {
		return owner{}, false
	}
	if o.Email == "" || o.Password == "" {
		return owner{}, false
	}
	if o.Name == "" {
		o.Name = o.Email
	}
	return o, true
}

// saveOwner writes the credentials, owner-readable only.
//
// Written before the account is created rather than after: an account that
// exists with a secret nobody kept is a control plane locked against its own
// machine, and the only way out of that is deleting the data directory.
func saveOwner(o owner) error {
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return fmt.Errorf("could not create the data directory: %w", err)
	}
	blob, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(ownerPath(), blob, 0o600); err != nil {
		return fmt.Errorf("could not save this machine's account: %w", err)
	}
	return nil
}
