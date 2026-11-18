package main

import (
	"strings"
	"testing"
)

// Автодополнение fish не должно отставать от диспетчера main(): каждая
// команда верхнего уровня и подкоманда dev в нём есть.
func TestFishCompletionCoversCommands(t *testing.T) {
	for _, c := range []string{"dev", "backlog", "version", "completion", "help"} {
		if !strings.Contains(fishCompletionScript, "'__fish_use_subcommand' -x -a "+c+" ") {
			t.Errorf("нет команды верхнего уровня %q", c)
		}
	}
	for _, c := range []string{"init", "up", "down", "status", "open", "logs", "secrets", "bridge",
		"update", "rollback", "doctor"} {
		if !strings.Contains(fishCompletionScript, "'__agctl_at dev' -x -a "+c+" ") {
			t.Errorf("нет подкоманды dev %q", c)
		}
	}
	for _, c := range []string{"serve", "migrate", "connect", "sync", "ax-runner", "bridge"} {
		if strings.Contains(fishCompletionScript, "'__fish_use_subcommand' -x -a "+c+" ") {
			t.Errorf("серверная команда %q в автодополнении agctl", c)
		}
	}
}
