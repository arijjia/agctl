//go:build server

package main

// bridge_stub.go — в серверном варианте guest моста к хосту нет: LocalOnly
// пресет моста сервер вырезает (internal/server/project_mcp.go), а хостовый
// localbridge поднимает только agctl dev.

import (
	"fmt"
	"os"
)

func cmdBridge(_ []string) {
	fmt.Fprintln(os.Stderr, "agctl bridge: мост к хосту есть только в локальном режиме — это серверная сборка guest (образ agctl-dsh-server)")
	os.Exit(2)
}
