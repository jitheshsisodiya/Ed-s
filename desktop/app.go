package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/jitheshsisodiya/Ed-s/backend/local"
	"github.com/jitheshsisodiya/Ed-s/client/agent"
)

// Version is stamped at build time with -ldflags "-X main.Version=v1.2.3".
var Version = "dev"

// App binds the NexusVPN client engine to the Wails frontend.
//
// All real work lives in the shared agent package, which nexusvpnctl uses
// too, so the GUI and the CLI cannot diverge in how they connect.
type App struct {
	ctx   context.Context
	agent *agent.Agent
	// startupErr is surfaced to the UI if the engine could not initialise.
	startupErr string

	// server is the control plane running inside this process. Nil when
	// this machine is a client of somebody else's.
	server *local.Server

	// mu guards the two fields below, which the tray goroutine and the
	// window's own thread both touch.
	mu sync.Mutex

	// quitting distinguishes "the user chose Quit" from "the user clicked
	// the window's X". Only the first should stop the process: closing the
	// window on a machine that hosts the control plane must not take
	// everyone else's network down.
	quitting bool

	// pendingInvite holds a code from a nexusvpn:// link, waiting for the
	// UI to be ready to act on it.
	pendingInvite string

	// trayPoll fans status changes out to the tray.
	trayPoll chan struct{}
}

// quit marks this as a real exit rather than a window close.
func (a *App) quit() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	wailsruntime.Quit(a.ctx)
}

// onSecondInstance runs in the copy already going when somebody launches
// NexusVPN again — from a shortcut, by opening an invite link, or by an
// installer asking the running copy to stand down.
func (a *App) onSecondInstance(data options.SecondInstanceData) {
	if QuitRequested(data.Args) {
		a.quit()
		return
	}
	if code := inviteFromArgs(data.Args); code != "" {
		a.offerInvite(code)
	}
	a.ShowWindow()
}

// QuitRequested reports whether a command line asks NexusVPN to stop.
//
// An upgrade cannot overwrite an executable Windows still has open, and this
// app is built to stay open — closing its window leaves it in the
// notification area, serving. So the installer runs the copy it is about to
// replace with this flag; the single-instance lock hands the flag to the one
// already running, which then shuts down the way the Quit menu item would,
// releasing the tunnel and stopping the control plane on its way out.
//
// Killing the process instead would leave whatever the kill switch had armed
// still armed, with nothing left running to take it down.
func QuitRequested(args []string) bool {
	for _, arg := range args {
		if arg == "--quit" || arg == "-quit" || arg == "/quit" {
			return true
		}
	}
	return false
}

// inviteFromArgs finds an invite handed over by the operating system.
//
// Only nexusvpn: arguments count. A bare invite code is a valid thing to
// paste into the join dialog but not something to act on because it appeared
// on a command line — that would make any stray argument look like an
// instruction to join a network.
func inviteFromArgs(args []string) string {
	for _, arg := range args {
		if strings.HasPrefix(strings.ToLower(arg), "nexusvpn:") {
			if code := agent.ParseInviteCode(arg); code != "" {
				return code
			}
		}
	}
	return ""
}

// offerInvite hands a code to the UI, which opens the join dialog with it
// filled in. Nothing joins on its own: a link someone sent is a suggestion,
// not a command, and the person clicking it should see which network it is
// before they are on it.
func (a *App) offerInvite(code string) {
	a.mu.Lock()
	a.pendingInvite = code
	a.mu.Unlock()
	a.logf("invite link received")
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "invite:offered", code)
	}
}

// TakePendingInvite returns a waiting invite code and forgets it, so a
// reload does not re-offer something already dismissed. The UI calls this on
// startup, when the event above would have fired before it was listening.
func (a *App) TakePendingInvite() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	code := a.pendingInvite
	a.pendingInvite = ""
	return code
}

// logf sends a line to the engine log the Diagnostics tab shows. Safe before
// the window exists — the tray starts first, and a message from that window
// has nowhere to go yet.
func (a *App) logf(format string, args ...any) {
	if a.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(a.ctx, "tunnel:log", fmt.Sprintf(format, args...))
}

// ShowWindow brings the window back from the tray.
func (a *App) ShowWindow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowShow(a.ctx)
	wailsruntime.WindowUnminimise(a.ctx)
}

// HideWindow puts the app back in the notification area.
func (a *App) HideWindow() {
	if a.ctx != nil {
		wailsruntime.WindowHide(a.ctx)
	}
}

// beforeClose intercepts the window's close button.
//
// Returning true cancels the close. The window hides instead, which is what
// every VPN client does and what this one has to do: the app may be serving
// other machines, and closing a window is not a request to disconnect them.
func (a *App) beforeClose(context.Context) bool {
	a.mu.Lock()
	quitting := a.quitting
	a.mu.Unlock()
	if quitting {
		return false
	}
	a.HideWindow()
	return true
}

// trayTick returns a channel that fires whenever the tray should refresh.
func (a *App) trayTick() <-chan struct{} {
	if a.trayPoll == nil {
		a.trayPoll = make(chan struct{}, 1)
		go func() {
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				select {
				case a.trayPoll <- struct{}{}:
				default:
				}
			}
		}()
	}
	return a.trayPoll
}

// role records whether this installation hosts the control plane or joins
// one hosted elsewhere.
//
// It is an explicit choice rather than something inferred, because both
// answers are correct and only the person installing it knows which they
// mean. Inferring it — say, hosting whenever port 8080 happens to be free —
// would mean an office of ten machines quietly running ten servers, each
// with its own set of accounts, none of which can see the others.
type role struct {
	// Host is true when this machine runs the control plane.
	Host bool `json:"host"`
	// Chosen distinguishes "join, and I meant it" from "not asked yet".
	Chosen bool `json:"chosen"`
	// Relay is true when this machine should carry traffic for pairs of
	// devices that cannot reach each other directly.
	//
	// Off unless asked for: it means this machine's connection carries
	// somebody else's data, which is fine when the somebody else is you, and
	// a thing to be asked about rather than assumed.
	Relay bool `json:"relay"`
	// Remote is true when this machine should be reachable from outside the
	// local network, which means asking the router to forward a port.
	//
	// Off unless asked for. It makes a machine reachable from the internet,
	// which is a decision belonging to whoever owns it — not something to
	// arrange on their behalf because it happens to be convenient.
	Remote bool `json:"remote"`
}

func rolePath() string { return filepath.Join(dataDir(), "role.json") }

func loadRole() role {
	blob, err := os.ReadFile(rolePath())
	if err != nil {
		return role{}
	}
	var r role
	if err := json.Unmarshal(blob, &r); err != nil {
		return role{}
	}
	return r
}

func saveRole(r role) error {
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(rolePath(), blob, 0o600)
}

// NewApp creates the application.
func NewApp() *App { return &App{} }

// startup captures the Wails context, starts the built-in control plane and
// initialises the engine.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Nothing was running to hand the flag to, so there is nothing to stop.
	// Started and stopped without ever showing a window, which is what an
	// installer running this on a machine where the app is already closed
	// should look like.
	if QuitRequested(os.Args[1:]) {
		a.quit()
		return
	}

	// An invite link is often what launches the app in the first place, so
	// the code is picked up before anything else can fail and swallow it.
	if code := inviteFromArgs(os.Args[1:]); code != "" {
		a.offerInvite(code)
	}

	ag, err := agent.New()
	if err != nil {
		a.startupErr = fmt.Sprintf("could not open configuration: %v", err)
		return
	}
	a.agent = ag

	// Only a machine that was told to host starts a control plane. A
	// machine that has not been asked yet starts nothing, so the first
	// screen can put the question before anything binds a port.
	if r := loadRole(); r.Chosen && r.Host {
		if err := a.startServer(); err != nil {
			a.startupErr = err.Error()
			return
		}
		if err := a.adoptThisMachine(); err != nil {
			// Not fatal, and not silent: the sign-in form is still there, and
			// this line is what explains why it appeared.
			a.logf("this machine could not sign into its own network: %v", err)
		}
	}
}

// startServer brings the built-in control plane up. Safe to call twice.
func (a *App) startServer() error {
	if a.server != nil {
		return nil
	}
	srv, err := local.Start(a.ctx, local.Options{
		DataDir:        dataDir(),
		OpenRouterPort: loadRole().Remote,
		RunRelay:       loadRole().Relay,
		Logf: func(format string, args ...any) {
			a.logf(format, args...)
		},
	})
	if err != nil {
		return fmt.Errorf("the built-in server did not start: %w", err)
	}
	a.server = srv

	// The machine hosting the control plane is the only one that knows its
	// address and its certificate, so it tells its own engine rather than
	// relying on whatever was written the last time somebody signed in.
	//
	// Without this, an installation that signed in before the server spoke
	// TLS kept using the old http address and got its connection closed by
	// the TLS listener, with an error naming a socket and nothing naming the
	// stale line in a config file that caused it.
	if a.agent != nil {
		if err := a.agent.UseServer(srv.BaseURL, srv.Fingerprint); err != nil {
			a.logf("could not record this machine's own server address: %v", err)
		}
	}
	return nil
}

// SetReachableFromAnywhere turns router port forwarding on or off.
//
// Takes effect on the next start rather than immediately, and says so:
// unwinding a running server's listeners and mappings in place is more ways
// to get it wrong than the convenience is worth.
func (a *App) SetReachableFromAnywhere(remote bool) error {
	r := loadRole()
	r.Remote = remote
	if err := saveRole(r); err != nil {
		return fmt.Errorf("could not save that choice: %w", err)
	}
	return nil
}

// SetRelayHere turns on carrying traffic for peers that cannot reach each
// other directly. Takes effect on the next start, like the setting above.
func (a *App) SetRelayHere(relay bool) error {
	r := loadRole()
	r.Relay = relay
	if err := saveRole(r); err != nil {
		return fmt.Errorf("could not save that choice: %w", err)
	}
	return nil
}

// SetServerRole records whether this machine hosts the control plane, and
// starts it if so. Called once, from the first screen.
func (a *App) SetServerRole(host bool) error {
	if err := saveRole(role{Host: host, Chosen: true, Remote: loadRole().Remote, Relay: loadRole().Relay}); err != nil {
		return fmt.Errorf("could not save that choice: %w", err)
	}
	if !host {
		return nil
	}
	if err := a.startServer(); err != nil {
		// The choice is rolled back rather than left recorded, so the next
		// launch asks again instead of silently failing to host forever.
		_ = saveRole(role{})
		return err
	}
	if err := a.adoptThisMachine(); err != nil {
		a.logf("this machine could not sign into its own network: %v", err)
	}
	a.startupErr = ""
	return nil
}

// dataDir is where this installation keeps its accounts, networks and keys:
// %APPDATA%\NexusVPN on Windows, ~/.config/NexusVPN elsewhere. Falling back
// to the working directory would scatter data wherever the app happened to
// be launched from.
func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "nexusvpn-data"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "NexusVPN")
}

// LocalServer describes the control plane running inside this app, so the UI
// can prefill its own address and tell the user what to give other machines.
type LocalServer struct {
	// Chosen reports that this machine has been told which role it plays.
	// False means the first screen still has to ask.
	Chosen bool `json:"chosen"`
	// Host reports that this machine is meant to run the control plane.
	Host bool `json:"host"`
	// Running reports whether the built-in server came up.
	Running bool `json:"running"`
	// URL is what this machine should use.
	URL string `json:"url"`
	// LANURL is what other machines on this network should use, empty if
	// this machine has no routable address.
	LANURL string `json:"lanUrl"`
	// PublicURL reaches this machine from outside the network. Empty means
	// the router would not open a port, or was never asked.
	PublicURL string `json:"publicUrl"`
	// Remote reports whether opening a port was asked for at all, so the UI
	// can tell "not requested" from "requested and refused" — which are
	// different problems with different answers.
	Remote bool `json:"remote"`
	// Relay reports whether this machine carries traffic for peers that
	// cannot reach each other directly.
	Relay bool `json:"relay"`
	// FirstRun reports that no account exists yet, so the UI offers to
	// create one instead of asking for a password nobody has set.
	FirstRun bool `json:"firstRun"`
}

// GetLocalServer reports the built-in control plane's state.
func (a *App) GetLocalServer() LocalServer {
	r := loadRole()
	if a.server == nil {
		return LocalServer{Chosen: r.Chosen, Host: r.Host, Remote: r.Remote, Relay: r.Relay}
	}
	return LocalServer{
		Chosen:    true,
		Host:      true,
		Running:   true,
		URL:       a.server.BaseURL,
		LANURL:    a.server.LANURL,
		PublicURL: a.server.PublicURL,
		Remote:    r.Remote,
		Relay:     r.Relay,
		FirstRun:  a.server.IsFirstRun(),
	}
}

// StartupPref reports whether launching at sign-in can be arranged on this
// platform, and whether it currently is.
type StartupPref struct {
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
}

// GetStartWithSystem reports the launch-at-sign-in setting.
func (a *App) GetStartWithSystem() StartupPref {
	return StartupPref{Supported: startupSupported, Enabled: startsWithSystem()}
}

// SetStartWithSystem turns launching at sign-in on or off.
func (a *App) SetStartWithSystem(on bool) error {
	if !startupSupported {
		return errors.New("starting at sign-in is not supported on this system")
	}
	return setStartsWithSystem(on)
}

// Register creates the first account and signs into it, which on a fresh
// install is one action rather than two.
func (a *App) Register(serverURL, email, password, displayName string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.agent.Register(a.ctx, serverURL, email, password, displayName)
}

// shutdown stops any running tunnel so the app never leaves a configured
// interface behind when the window closes.
func (a *App) shutdown(context.Context) {
	if a.agent != nil {
		a.agent.Disconnect()
	}
	// Stopped after the tunnel, so a device that is disconnecting can still
	// tell the control plane it is going.
	if a.server != nil {
		a.server.Stop()
	}
}

// ready reports whether the engine initialised.
func (a *App) ready() error {
	if a.agent == nil {
		if a.startupErr != "" {
			return errors.New(a.startupErr)
		}
		return errors.New("the client engine is not initialised")
	}
	return nil
}

// AppInfo is returned to the UI on startup.
type AppInfo struct {
	Version string `json:"version"`
	Error   string `json:"error"`
}

// GetAppInfo reports the app version and any startup failure.
func (a *App) GetAppInfo() AppInfo {
	return AppInfo{Version: Version, Error: a.startupErr}
}

// GetSession reports the stored login state so the UI can choose a screen.
func (a *App) GetSession() (agent.Session, error) {
	if err := a.ready(); err != nil {
		return agent.Session{}, err
	}
	return a.agent.Session()
}

// Login authenticates against a control plane. If the account requires MFA
// and no code was given, the error is "mfa_required" and the UI re-submits
// with a code.
func (a *App) Login(serverURL, email, password, mfaCode string) error {
	if err := a.ready(); err != nil {
		return err
	}
	err := a.agent.Login(a.ctx, serverURL, email, password, mfaCode)
	if errors.Is(err, agent.ErrMFARequired) {
		return errors.New("mfa_required")
	}
	return err
}

// Logout clears the session (and disconnects first).
func (a *App) Logout() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.agent.Logout(a.ctx)
}

// ListNetworks returns the account's networks.
func (a *App) ListNetworks() ([]agent.Network, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.agent.ListNetworks(a.ctx)
}

// CreateNetwork creates a network.
func (a *App) CreateNetwork(name, description, cidr string) (agent.Network, error) {
	if err := a.ready(); err != nil {
		return agent.Network{}, err
	}
	return a.agent.CreateNetwork(a.ctx, name, description, cidr)
}

// JoinNetwork joins a network by invite code.
func (a *App) JoinNetwork(inviteCode string) (agent.Network, error) {
	if err := a.ready(); err != nil {
		return agent.Network{}, err
	}
	return a.agent.JoinNetwork(a.ctx, inviteCode)
}

// Connect brings up the tunnel. It returns once the interface is up and
// registered; peer negotiation continues in the background and is reported
// through GetStatus and the "tunnel:log" event.
func (a *App) Connect(networkID string) error {
	if err := a.ready(); err != nil {
		return err
	}

	err := a.agent.Connect(networkID, agent.ConnectOptions{
		ClientVersion: Version,
		Logf: func(format string, args ...any) {
			a.logf(format, args...)
		},
	})
	if err != nil {
		return err
	}

	// Tell the UI when the tunnel stops for any reason, so it can drop back
	// to the disconnected state without polling for it.
	go func() {
		a.agent.Wait()
		wailsruntime.EventsEmit(a.ctx, "tunnel:disconnected")
	}()

	wailsruntime.EventsEmit(a.ctx, "tunnel:connected")
	return nil
}

// Disconnect stops the tunnel.
func (a *App) Disconnect() error {
	if err := a.ready(); err != nil {
		return err
	}
	a.agent.Disconnect()
	return nil
}

// GetStatus returns the live tunnel state for the UI to render.
func (a *App) GetStatus() agent.Status {
	if a.agent == nil {
		return agent.Status{}
	}
	return a.agent.Status()
}

// CopyToClipboard puts text on the system clipboard. The UI uses it for the
// one action people reach for constantly: copying a peer's virtual IP to
// paste into a game server browser, an RDP client or a file share.
func (a *App) CopyToClipboard(text string) error {
	return wailsruntime.ClipboardSetText(a.ctx, text)
}

// InviteLink renders an invite code as the shareable link form. The UI puts
// it in a QR code so a phone can join by pointing its camera at the screen,
// and on the clipboard so it can be pasted into a chat.
//
// The formatting lives in the shared agent package so a link produced here
// is a link nexusvpnctl and the mobile apps accept.
func (a *App) InviteLink(code string) string {
	return agent.InviteLink(code)
}

// RotateDeviceKey generates a fresh device keypair and returns its public key.
func (a *App) RotateDeviceKey() (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.agent.RotateDeviceKey()
}

// SetDeviceName renames this device.
func (a *App) SetDeviceName(name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.agent.SetDeviceName(name)
}

// UseExitNode routes all of this machine's traffic through a peer that has
// offered to carry it. This is the only mode in which NexusVPN changes what
// the rest of the internet sees of this machine.
func (a *App) UseExitNode(deviceID string, opts agent.ExitNodeOptions) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.agent.UseExitNode(deviceID, opts)
}

// StopUsingExitNode restores ordinary split-tunnel routing and lifts any
// block the kill switch was holding.
func (a *App) StopUsingExitNode() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.agent.StopUsingExitNode()
}

// ClaimPairing redeems a pairing code taken from the machine hosting the
// network, and reports which network to connect to.
//
// The same code the phone scans. A second computer has no camera pointed at
// the first one's screen, so it pastes the link instead — but what it does
// with it, and what it is trusting, is identical: a single-use code that
// expires in minutes and carries both the server's address and the
// certificate that server has to present.
func (a *App) ClaimPairing(link string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	return a.agent.ClaimPairing(a.ctx, link)
}

// AdoptThisMachine signs this app back into the network it hosts.
//
// Startup does this by itself, so the button this backs is only ever reached
// after somebody signed out — which on a machine hosting its own network is
// a dead end otherwise, since the credentials it would then be asking for are
// ones nobody ever chose.
func (a *App) AdoptThisMachine() error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.adoptThisMachine()
}

// adoptThisMachine signs this app into the control plane it is hosting,
// creating the account on first run.
//
// Nobody is asked to invent a login for their own PC. See owner.go for why
// that is not a shortcut: the credential this generates is stronger than any
// a person would have chosen, and the thing a typed password would have
// protected is already protected by the same directory permissions.
//
// Errors are reported, not swallowed, and the sign-in form remains for the
// cases this cannot cover — an account somebody made by hand, or a machine
// pointed at a control plane it does not host.
func (a *App) adoptThisMachine() error {
	if a.agent == nil || a.server == nil {
		return errors.New("this machine is not hosting a network")
	}
	if s, err := a.agent.Session(); err == nil && s.LoggedIn {
		return nil
	}

	o, saved := loadOwner()
	if a.server.IsFirstRun() {
		if !saved {
			hostname, _ := os.Hostname()
			fresh, err := newOwner(hostname)
			if err != nil {
				return err
			}
			o = fresh
		}
		// Saved first: an account that exists with a secret nobody kept is a
		// control plane locked against the machine running it.
		if err := saveOwner(o); err != nil {
			return err
		}
		return a.agent.Register(a.ctx, a.server.BaseURL, o.Email, o.Password, o.Name)
	}

	if !saved {
		// An account exists that this app did not make — somebody registered
		// by hand, or carried a data directory across. Guessing is not
		// available, so the form asks.
		return errors.New("this machine already has an account that has to be signed into")
	}
	return a.agent.Login(a.ctx, a.server.BaseURL, o.Email, o.Password, "")
}

// StartPairing issues a code that signs another device in as this account, on
// this network, without anything being typed on it.
func (a *App) StartPairing(networkID string) (agent.PairingLink, error) {
	if err := a.ready(); err != nil {
		return agent.PairingLink{}, err
	}
	// Only this process knows its own certificate, so it supplies it rather
	// than letting the agent guess.
	fingerprint := ""
	if a.server != nil {
		fingerprint = a.server.Fingerprint
	}
	link, err := a.agent.StartPairing(a.ctx, networkID, fingerprint)
	if err != nil {
		return agent.PairingLink{}, err
	}
	return *link, nil
}
