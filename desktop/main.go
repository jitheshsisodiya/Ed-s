package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	// The tray runs alongside Wails on its own goroutine. It is started
	// before Run because Run blocks for the lifetime of the app.
	go startTray(app)

	err := wails.Run(&options.App{
		Title:     "NexusVPN",
		Width:     860,
		Height:    640,
		MinWidth:  680,
		MinHeight: 480,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Matches the deck's own ground so there is no flash of a foreign
		// colour before the webview paints.
		BackgroundColour: &options.RGBA{R: 0x05, G: 0x08, B: 0x0F, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		// Closing the window hides it rather than quitting: this app may be
		// serving other machines, and an X is not a request to disconnect
		// them. Quit lives in the tray menu.
		OnBeforeClose:     app.beforeClose,
		HideWindowOnClose: true,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
