//go:build !windows

package main

import (
	"github.com/agent-substrate/env/guest"
	"google.golang.org/grpc"
)

// newGuestServer — guest-сервисы Substrate (process+filesystem) для ax ssh.
// Пакет guest собирается только на Unix (POSIX-сигналы), поэтому вынесен
// сюда: Windows-сборка agctl (локальный режим) не должна от него зависеть.
func newGuestServer(workDir string) (*grpc.Server, func(), error) {
	cfg := guest.DefaultConfig()
	cfg.Workspace = workDir
	cfg.LogDir = "/tmp/agctl-guest-logs"
	return guest.NewServer(cfg)
}
