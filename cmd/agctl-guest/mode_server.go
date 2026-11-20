//go:build server

package main

// mode_server.go — серверный вариант guest (сборка -tags server): бинарь
// образов agctl-dsh-server (рантаймы docker|k8s|ax у agctl-server). ax-runner
// и init-chat-home; моста к хосту нет — он существует только в локальном
// режиме (сервер вырезает LocalOnly-пресет моста).

import "github.com/arijjia/agctl/internal/cli"

// guestVariant — метка варианта в `agctl version` (guest server).
const guestVariant = "server"

// modeRows — команды серверного варианта.
func modeRows() []cli.UsageRow {
	return []cli.UsageRow{
		{Name: "ax-runner", Desc: []string{"PID 1 песочницы Google AX / Agent Substrate"}},
		{Name: "init-chat-home", Desc: []string{"init-контейнер пода k8s: подготовить DSH_HOME на томе (--home, --refresh-provider)"}},
	}
}
