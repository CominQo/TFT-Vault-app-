package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// frontend/models contains a go.mod marker file, so Go treats it as a separate module
// and skips it here. The .glb models are never embedded; see models.go.
//
//go:embed all:frontend
var assets embed.FS

func main() {
	app := NewApp(assets)
	err := wails.Run(&options.App{
		Title:            "TFT Collection",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true, // custom title bar is drawn in frontend/index.html
		BackgroundColour: &options.RGBA{R: 7, G: 10, B: 15, A: 255},
		// Embedded UI first; anything not found there (/models/*.glb) goes to the disk/remote handler.
		AssetServer: &assetserver.Options{Assets: assets, Handler: newModelsHandler()},
		OnStartup:   app.startup,
		Bind:        []interface{}{app},
		Windows:     &windows.Options{Theme: windows.Dark},
	})
	if err != nil {
		println("error:", err.Error())
	}
}
