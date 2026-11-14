//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detach — фоновый процесс agctl dev serve переживает закрытие терминала.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
