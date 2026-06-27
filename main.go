package main

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/user/flow/backend"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func setupLogFile() (*os.File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("home dir: %w", err)
	}
	logDir := filepath.Join(home, ".flow")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", logDir, err)
	}
	f, err := os.OpenFile(filepath.Join(logDir, "flow.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	return f, nil
}

func main() {
	if f, err := setupLogFile(); err != nil {
		log.Printf("warning: log file unavailable: %v", err)
	} else {
		defer f.Close()
	}

	app := backend.NewApp()

	// Hand the floating HUD server the built frontend assets (dist subtree).
	if distFS, err := fs.Sub(assets, "frontend/dist"); err != nil {
		log.Printf("warning: HUD assets unavailable: %v", err)
	} else {
		app.SetHUDAssets(distFS)
	}

	err := wails.Run(&options.App{
		Title:             "Flow",
		Width:             1100,
		Height:            780,
		MinWidth:          800,
		MinHeight:         560,
		BackgroundColour:  &options.RGBA{R: 28, G: 25, B: 23, A: 255},
		AssetServer:       &assetserver.Options{Assets: assets},
		HideWindowOnClose: true,
		OnStartup:         app.Startup,
		OnShutdown:        app.Shutdown,
		Bind:              []interface{}{app},
		Mac: &mac.Options{
			TitleBar: &mac.TitleBar{
				TitlebarAppearsTransparent: true,
				HideTitle:                  true,
				HideTitleBar:               false,
				FullSizeContent:            true,
			},
			WebviewIsTransparent: true,
			WindowIsTranslucent:  false,
			Appearance:           mac.NSAppearanceNameDarkAqua,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}


