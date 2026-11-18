//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup — дочерний процесс в своей группе: сигнал уходит всему
// дереву (dsh и его MCP-процессы).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalGroup — сигнал группе процессов команды (fallback — самому процессу).
func signalGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}
