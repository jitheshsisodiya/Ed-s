package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

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

	// server is the control plane running inside this process, so a person
	// with one machine and no patience for Docker still has somewhere for
	// their accounts and networks to live. Nil when the app was pointed at
	// a server somebody else runs.
	server *local.Server
}

// NewApp creates the application.
func NewApp() *App { return &App{} }

// startup captures the Wails context, starts the built-in control plane and
// initialises the engine.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	ag, err := agent.New()
	if err != nil {
		a.startupErr = fmt.Sprintf("could not open configuration: %v", err)
		return
	}
	a.agent = ag

	// The server comes up before the UI asks anything, so the sign-in
	// screen is talking to something live by the time it renders. A
	// failure here is not fatal: the app still works pointed at a server
	// running somewhere else, which is what an organisation would do.
	srv, err := local.Start(ctx, local.Options{
		DataDir: dataDir(),
		Logf: func(format string, args ...any) {
			wailsruntime.EventsEmit(a.ctx, "tunnel:log", fmt.Sprintf(format, args...))
		},
	})
	if err != nil {
		a.startupErr = fmt.Sprintf("the built-in server did not start: %v", err)
		return
	}
	a.server = srv
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
	// Running reports whether the built-in server came up.
	Running bool `json:"running"`
	// URL is what this machine should use.
	URL string `json:"url"`
	// LANURL is what other machines on this network should use, empty if
	// this machine has no routable address.
	LANURL string `json:"lanUrl"`
	// FirstRun reports that no account exists yet, so the UI offers to
	// create one instead of asking for a password nobody has set.
	FirstRun bool `json:"firstRun"`
}

// GetLocalServer reports the built-in control plane's state.
func (a *App) GetLocalServer() LocalServer {
	if a.server == nil {
		return LocalServer{}
	}
	return LocalServer{
		Running:  true,
		URL:      a.server.BaseURL,
		LANURL:   a.server.LANURL,
		FirstRun: a.server.IsFirstRun(),
	}
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
			wailsruntime.EventsEmit(a.ctx, "tunnel:log", fmt.Sprintf(format, args...))
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
