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
	err := wails.Run(&options.App{
		Title:     "NexusVPN",
		Width:     620,
		Height:    800,
		MinWidth:  460,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		// Matches the light theme's page background so there is no flash of
		// a foreign colour before the webview paints.
		BackgroundColour: &options.RGBA{R: 0xF6, G: 0xF7, B: 0xF9, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
