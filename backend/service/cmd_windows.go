//go:build windows

package service

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: syscall doesn't export it, and pulling in
// golang.org/x/sys just for one constant isn't worth it.
const createNoWindow = 0x08000000

// command builds an exec.Cmd that runs without popping up a console window.
// A Wails GUI process has no console of its own, so Windows would allocate a
// visible one for every .bat/.cmd child we spawn.
func command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd
}
