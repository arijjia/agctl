//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// Раннер AX работает только в linux-песочнице; на Windows — заглушки, чтобы
// бинарь собирался (release_local).
func setProcessGroup(*exec.Cmd) {}

func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
