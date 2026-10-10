package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name: "counterdraft",
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Frameless, always-on-top, fully transparent overlay; the HTML paints the
	// visible chrome. BackgroundTypeTransparent requires the alpha-0 colour.
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "counterdraft",
		Width:            360,
		Height:           560,
		MinWidth:         300,
		MinHeight:        320,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
		Windows: application.WindowsWindow{
			DisableFramelessWindowDecorations: true,
		},
		Linux: application.LinuxWindow{
			WindowIsTranslucent: true,
		},
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTransparent,
		},
	})

	// Register the service after the window exists so it can hold references to
	// both the application and its window.
	app.RegisterService(application.NewService(NewApp(app, window)))

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
