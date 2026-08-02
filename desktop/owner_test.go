package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The whole justification for not asking anybody to type a password is that
// the generated one is better than the one they would have chosen. If it is
// short, predictable, or the same on two machines, that argument is gone and
// a self-hosted control plane on the open internet is sitting behind it.
func TestTheGeneratedSecretIsWorthNotTyping(t *testing.T) {
	first, err := newOwner("desktop-pc")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newOwner("desktop-pc")
	if err != nil {
		t.Fatal(err)
	}

	if first.Password == second.Password {
		t.Fatal("two machines got the same password, so knowing one is knowing all of them")
	}

	raw, err := base64.RawURLEncoding.DecodeString(first.Password)
	if err != nil {
		t.Fatalf("the password is not the encoding it claims to be: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("password carries %d bytes of entropy, want 32", len(raw))
	}

	// The control plane refuses anything shorter, and finding that out at
	// first run would mean an install that cannot sign into itself.
	if len(first.Password) < 12 {
		t.Fatalf("password is %d characters, below the server's minimum", len(first.Password))
	}
}

// The server requires an "@" and a display name, and rejects the request
// outright without them. A hostname is not under this code's control.
func TestEveryHostnameProducesAnAcceptableAccount(t *testing.T) {
	for _, hostname := range []string{
		"DESKTOP-4F2A1",
		"jithesh's pc",
		"maison_étrange",
		"---",
		"",
		strings.Repeat("a", 200),
	} {
		o, err := newOwner(hostname)
		if err != nil {
			t.Fatalf("hostname %q: %v", hostname, err)
		}
		if !strings.Contains(o.Email, "@") {
			t.Errorf("hostname %q produced email %q, which the server rejects", hostname, o.Email)
		}
		if strings.TrimSpace(o.Name) == "" {
			t.Errorf("hostname %q produced an empty display name, which the server rejects", hostname)
		}
		if o.Email != strings.ToLower(o.Email) {
			t.Errorf("hostname %q produced %q; the server lowercases, so a later "+
				"sign-in would not match", hostname, o.Email)
		}
		if strings.ContainsAny(o.Email, " '_é") {
			t.Errorf("hostname %q leaked through into %q", hostname, o.Email)
		}
	}
}

func TestOwnerSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	want, err := newOwner("desktop-pc")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(want); err != nil {
		t.Fatal(err)
	}

	got, ok := loadOwner()
	if !ok {
		t.Fatal("nothing was stored, so this machine can never sign into its own network again")
	}
	if got.Email != want.Email || got.Password != want.Password {
		t.Fatal("what came back is not what went in")
	}
}

// The file holds a credential to a control plane that may be reachable from
// the internet. Group or world readable would be a real downgrade from the
// password prompt this replaces.
func TestTheStoredSecretIsNotReadableByOthers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	o, err := newOwner("desktop-pc")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(o); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(ownerPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("owner.json is mode %04o; anybody else on this machine can read the password", perm)
	}
}

func TestUnreadableCredentialsAreTreatedAsAbsent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, contents := range []string{
		"not json at all",
		`{"email":"a@b","password":""}`,
		`{"email":"","password":"secret"}`,
	} {
		if err := os.WriteFile(ownerPath(), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok := loadOwner(); ok {
			t.Fatalf("%q was offered as usable credentials", contents)
		}
	}
}

// Nothing about the account may be derived from the hostname alone, or
// anybody who can see a machine's name on the network knows half of it.
func TestTheSecretIsNotDerivedFromTheHostname(t *testing.T) {
	o, err := newOwner("desktop-pc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(o.Password), "desktop") {
		t.Fatal("the hostname appears in the password")
	}
}

func TestOwnerPathSitsWithTheRestOfTheData(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if got, want := filepath.Dir(ownerPath()), dataDir(); got != want {
		t.Fatalf("credentials stored in %q, data in %q", got, want)
	}
}
