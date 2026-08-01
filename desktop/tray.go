package main

import (
	"fmt"
	"sync"

	"github.com/energye/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/jitheshsisodiya/Ed-s/client/agent"
)

// tray puts NexusVPN in the notification area, where every other VPN lives.
//
// This is not decoration. A VPN is a thing you switch on and then forget
// about for eight hours, and an app that has to stay in the taskbar to keep
// working is an app nobody keeps running. It matters twice over here,
// because on a machine that hosts the control plane, closing the window
// would take everyone else's network down with it.
//
// Wails v2 has no tray of its own — it is planned for v3 — so this drives
// systray alongside it. The library takes over its own goroutine and calls
// back on events, which is why every handler here does nothing but hand
// work to the app.
type tray struct {
	app *App

	mu      sync.Mutex
	toggle  *systray.MenuItem
	status  *systray.MenuItem
	address *systray.MenuItem

	// lastPhase avoids rewriting the icon and title on every poll, which
	// on Windows makes the tray flicker.
	lastPhase string
}

// startTray runs the tray until the app quits.
//
// The recover is not defensive padding. There is no portable guarantee that a
// notification area exists: on Linux the tray is a D-Bus service, and systray
// panics rather than returning an error when the session bus is missing or
// refuses the export — which is the normal state of a headless session, a
// minimal desktop, or a machine where the user's bus has died. That panic
// arrives on this goroutine and would otherwise take the entire application
// with it. Losing the tray is a nuisance; losing the VPN because of it is not
// acceptable, so the app carries on windowed.
func startTray(app *App) {
	defer func() {
		if r := recover(); r != nil {
			app.logf("no notification area available (%v); running windowed", r)
		}
	}()
	t := &tray{app: app}
	systray.Run(t.onReady, func() {})
}

func (t *tray) onReady() {
	systray.SetIcon(trayIcon(phaseIdle))
	systray.SetTitle("NexusVPN")
	systray.SetTooltip("NexusVPN — not connected")

	// The status line is a disabled item rather than a tooltip: a tooltip
	// needs a hover and a wait, and the one thing somebody opens this menu
	// to learn is whether it is up.
	t.status = systray.AddMenuItem("Not connected", "")
	t.status.Disable()
	t.address = systray.AddMenuItem("", "Copy this machine's address")
	t.address.Hide()
	systray.AddSeparator()

	t.toggle = systray.AddMenuItem("Connect", "Connect to your last network")
	show := systray.AddMenuItem("Open NexusVPN", "Show the window")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "Stop NexusVPN entirely")

	t.toggle.Click(func() { go t.onToggle() })
	t.address.Click(func() { go t.onCopyAddress() })
	show.Click(func() { t.app.ShowWindow() })

	// Quitting is the only way out that stops the control plane, so it is
	// the only one that ends the process. Closing the window hides it.
	quit.Click(func() {
		t.app.quitting = true
		wailsruntime.Quit(t.app.ctx)
	})

	go t.watch()
}

// watch keeps the menu in step with the tunnel.
func (t *tray) watch() {
	for range t.app.trayTick() {
		t.refresh()
	}
}

func (t *tray) refresh() {
	if t.app.agent == nil {
		return
	}
	status := t.app.agent.Status()

	phase := phaseIdle
	label := "Not connected"
	switch {
	case !status.Connected:
	case status.State == agent.StateDropped:
		phase, label = phaseDropped, "Tunnel lost"
	case status.State == agent.StateHandshaking:
		phase, label = phaseLinking, "Connecting…"
	default:
		online := 0
		for _, p := range status.Peers {
			if p.Quality != agent.QualityOffline {
				online++
			}
		}
		phase = phaseActive
		label = fmt.Sprintf("Connected — %d of %d machines", online, len(status.Peers))
	}

	t.mu.Lock()
	changed := phase != t.lastPhase
	t.lastPhase = phase
	t.mu.Unlock()

	if changed {
		systray.SetIcon(trayIcon(phase))
		if status.Connected {
			t.toggle.SetTitle("Disconnect")
		} else {
			t.toggle.SetTitle("Connect")
		}
	}

	t.status.SetTitle(label)
	systray.SetTooltip("NexusVPN — " + label)

	if status.Connected && status.VirtualIP != "" {
		t.address.SetTitle("Copy " + status.VirtualIP)
		t.address.Show()
	} else {
		t.address.Hide()
	}
}

func (t *tray) onToggle() {
	if t.app.agent == nil {
		return
	}
	if t.app.agent.Status().Connected {
		_ = t.app.Disconnect()
		return
	}
	// Connecting needs a network to connect to, and the tray does not have
	// the UI to choose one. Opening the window is the honest response: it
	// is where that choice lives.
	t.app.ShowWindow()
}

func (t *tray) onCopyAddress() {
	if t.app.agent == nil {
		return
	}
	if ip := t.app.agent.Status().VirtualIP; ip != "" {
		_ = t.app.CopyToClipboard(ip)
	}
}
