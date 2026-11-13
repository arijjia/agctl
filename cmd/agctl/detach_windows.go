//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach — фоновый процесс без консоли, в своей группе (Ctrl+C терминала
// его не задевает).
func detach(cmd *exec.Cmd) {
	const createNewProcessGroup, detachedProcess = 0x00000200, 0x00000008
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
