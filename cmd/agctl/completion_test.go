package main

import (
	"strings"
	"testing"
)

// Автодополнение fish не должно отставать от диспетчера main(): каждая
// пользовательская команда верхнего уровня и подкоманда dev в нём есть.
func TestFishCompletionCoversCommands(t *testing.T) {
	for _, c := range []string{"serve", "migrate", "seed-demo-users", "backlog", "connect", "local",
		"sync", "dev", "bridge", "config", "version", "completion", "help"} {
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
}
