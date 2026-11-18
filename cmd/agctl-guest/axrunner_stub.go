//go:build !server

package main

// axrunner_stub.go — в локальном варианте guest раннера AX нет: PID 1
// песочницы Substrate нужен только серверному образу agctl-dsh-server
// (сборка `-tags server`, make go_linux_server).

import (
	"fmt"
	"os"
)

func cmdAXRunner(_ []string) {
	fmt.Fprintln(os.Stderr, "agctl ax-runner: команда есть только в серверной сборке guest (образ agctl-dsh-server, `make go_linux_server`)")
	os.Exit(2)
}
