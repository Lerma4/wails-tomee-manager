//go:build !windows

package service

import "os/exec"

func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
