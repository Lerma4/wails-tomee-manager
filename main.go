package main

import (
	"embed"
	"tomee-manager/backend/service"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Initialize services
	storageService := service.NewStorageService()
	if err := storageService.Init(); err != nil {
		println("Error initializing storage:", err.Error())
		return
	}

	tomeeService := service.NewTomEEService(storageService)
	warService := service.NewWarService(storageService)
	mavenService := service.NewMavenService(storageService)

	// Create an instance of the app structure
	app := NewApp(tomeeService, mavenService)

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "tomee-manager",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
			storageService,
			tomeeService,
			warService,
			mavenService,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
