//go:build !server

package main

// mode_local.go — локальный вариант guest (сборка без тегов): бинарь образа
// agctl-dsh (локальный режим agctl dev). Только мост к хосту: ax-runner и
// init-chat-home в этом образе не нужны и не собираются.

import "github.com/arijjia/agctl/internal/cli"

// guestVariant — метка варианта в `agctl version` (guest local).
const guestVariant = "local"

// modeRows — команды локального варианта.
func modeRows() []cli.UsageRow {
	return []cli.UsageRow{
		{Name: "bridge", Desc: []string{"мост к хосту (локальный режим): list | call ИНСТР k=v | mcp"}},
	}
}
