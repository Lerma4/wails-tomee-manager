package main

import (
	"context"
	"fmt"
	"tomee-manager/backend/service"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx          context.Context
	tomeeService *service.TomEEService
}

// NewApp creates a new App application struct
func NewApp(tomeeService *service.TomEEService) *App {
	return &App{
		tomeeService: tomeeService,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.tomeeService.SetContext(ctx)
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

func (a *App) SelectDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Directory",
	})
}

func (a *App) SelectWarFile() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select WAR File",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "WAR Files",
				Pattern:     "*.war",
			},
			{
				DisplayName: "All Files",
				Pattern:     "*.*",
			},
		},
	})
}
