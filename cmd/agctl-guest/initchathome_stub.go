//go:build !server

package main

// initchathome_stub.go — в локальном варианте guest нет init-chat-home:
// init-контейнер пода k8s сеется серверным образом agctl-dsh-server
// (сборка `-tags server`, make go_linux_server).

import (
	"fmt"
	"os"
)

func cmdInitChatHome(_ []string) {
	fmt.Fprintln(os.Stderr, "agctl init-chat-home: команда есть только в серверной сборке guest (образ agctl-dsh-server, `make go_linux_server`)")
	os.Exit(2)
}
