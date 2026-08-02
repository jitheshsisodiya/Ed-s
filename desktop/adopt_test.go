package main

import (
	"context"
	"testing"

	"github.com/jitheshsisodiya/Ed-s/backend/local"
	"github.com/jitheshsisodiya/Ed-s/client/agent"
)

// The claim being tested is "you do not sign in on your own PC". It is worth
// a real server rather than a fake: the account has to be one the control
// plane actually accepts, on the first run, with nobody typing anything.
func TestTheHostingMachineSignsItselfIn(t *testing.T) {
	app, srv := hostingApp(t)

	if !srv.IsFirstRun() {
		t.Fatal("a fresh server did not report itself as a first run")
	}

	if err := app.adoptThisMachine(); err != nil {
		t.Fatalf("the machine hosting the network could not sign into it: %v", err)
	}

	s, err := app.agent.Session()
	if err != nil {
		t.Fatal(err)
	}
	if !s.LoggedIn {
		t.Fatal("no session, so the app would still be showing a sign-in form")
	}
	if srv.IsFirstRun() {
		t.Fatal("no account was created")
	}
}

// Restarting must not create a second account or lock the machine out. This
// is where storing the secret earns its place: without it, run two is a
// control plane that exists and cannot be entered.
func TestASecondStartSignsBackInRatherThanRegistering(t *testing.T) {
	app, srv := hostingApp(t)

	if err := app.adoptThisMachine(); err != nil {
		t.Fatal(err)
	}
	first, err := app.agent.Session()
	if err != nil {
		t.Fatal(err)
	}

	// A restart: same data directory, same server, a fresh agent holding no
	// session at all.
	restarted, err := agent.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.agent = restarted

	if err := app.adoptThisMachine(); err != nil {
		t.Fatalf("could not sign back in after a restart: %v", err)
	}

	second, err := app.agent.Session()
	if err != nil {
		t.Fatal(err)
	}
	if !second.LoggedIn {
		t.Fatal("the restarted app was left signed out")
	}
	if second.Email != first.Email {
		t.Fatalf("restart signed in as %q, was %q — a second account was made",
			second.Email, first.Email)
	}
	_ = srv
}

// An account this app did not create cannot be guessed at, and pretending
// otherwise would leave somebody staring at a screen that never resolves.
// The failure has to be reportable so the sign-in form comes back.
func TestAnAccountThisAppDidNotMakeFallsBackToAsking(t *testing.T) {
	app, srv := hostingApp(t)

	if err := app.agent.Register(context.Background(), srv.BaseURL,
		"someone@example.com", "correct-horse-battery", "Someone"); err != nil {
		t.Fatal(err)
	}
	if err := app.agent.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := app.adoptThisMachine(); err == nil {
		t.Fatal("claimed to have signed into an account it has no password for")
	}
}

// Calling it twice must not register twice. Startup and the role screen can
// both reach it on the same launch.
func TestAdoptingIsSafeToRepeat(t *testing.T) {
	app, _ := hostingApp(t)

	if err := app.adoptThisMachine(); err != nil {
		t.Fatal(err)
	}
	if err := app.adoptThisMachine(); err != nil {
		t.Fatalf("a second call failed: %v", err)
	}
}

// The other half of "no signing in": a second computer joins by pasting the
// code the first one hands out, exactly as a phone joins by scanning it. No
// address, no email, no password — and no camera either.
func TestASecondComputerJoinsFromAPastedLink(t *testing.T) {
	app, srv := hostingApp(t)
	if err := app.adoptThisMachine(); err != nil {
		t.Fatal(err)
	}

	network, err := app.agent.CreateNetwork(context.Background(), "Home", "", "10.77.0.0/24")
	if err != nil {
		t.Fatal(err)
	}

	link, err := app.StartPairing(network.ID)
	if err != nil {
		t.Fatalf("could not issue a pairing code: %v", err)
	}
	if link.URL == "" {
		t.Fatal("the code has no link to paste anywhere")
	}
	if link.Fingerprint == "" {
		t.Fatal("the link carries no certificate, so the joining machine has " +
			"nothing to pin and would trust whatever answers")
	}

	second := secondComputer(t)
	joined, err := second.ClaimPairing(link.URL)
	if err != nil {
		t.Fatalf("the second computer could not join: %v", err)
	}
	if joined != network.ID {
		t.Fatalf("joined network %q, expected %q", joined, network.ID)
	}

	s, err := second.agent.Session()
	if err != nil {
		t.Fatal(err)
	}
	if !s.LoggedIn {
		t.Fatal("the second computer redeemed a code and was still signed out")
	}
	if s.ServerURL == "" {
		t.Fatal("no server was recorded, so nothing was learned from the link")
	}
	_ = srv
}

// A code is good once. A second computer that gets hold of a spent one has to
// be told so, not quietly signed in.
func TestAPastedLinkCannotBeUsedTwice(t *testing.T) {
	app, _ := hostingApp(t)
	if err := app.adoptThisMachine(); err != nil {
		t.Fatal(err)
	}
	network, err := app.agent.CreateNetwork(context.Background(), "Home", "", "10.77.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	link, err := app.StartPairing(network.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := secondComputer(t).ClaimPairing(link.URL); err != nil {
		t.Fatal(err)
	}
	if _, err := secondComputer(t).ClaimPairing(link.URL); err == nil {
		t.Fatal("the same code signed in a second machine")
	}
}

func TestPastingSomethingThatIsNotAPairingLinkIsRefused(t *testing.T) {
	second := secondComputer(t)
	for _, input := range []string{
		"",
		"   ",
		"https://example.com/pair?s=x&t=y",
		"nexusvpn://join?code=ABC123",
		"nexusvpn://pair?s=https://192.168.1.5:8080",
	} {
		if _, err := second.ClaimPairing(input); err == nil {
			t.Errorf("%q was accepted as a pairing link", input)
		}
	}
}

// secondComputer is an App with its own configuration directory and no
// server of its own — the machine on the other side of the room.
func secondComputer(t *testing.T) *App {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("APPDATA", dir)

	ag, err := agent.New()
	if err != nil {
		t.Fatal(err)
	}
	return &App{ctx: context.Background(), agent: ag}
}

// hostingApp builds an App that hosts a real control plane, with its
// configuration and data confined to this test.
func hostingApp(t *testing.T) (*App, *local.Server) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	// Windows keeps its per-user data under APPDATA, which os.UserConfigDir
	// reads instead of the two above.
	t.Setenv("APPDATA", dir)

	srv, err := local.Start(context.Background(), local.Options{
		DataDir: t.TempDir(),
		// Loopback only: binding every interface on a build machine is rude,
		// and in CI sometimes refused outright.
		Host: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("start the control plane: %v", err)
	}
	t.Cleanup(srv.Stop)

	ag, err := agent.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := ag.UseServer(srv.BaseURL, srv.Fingerprint); err != nil {
		t.Fatal(err)
	}

	return &App{ctx: context.Background(), agent: ag, server: srv}, srv
}
