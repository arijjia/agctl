package main

import (
	"strings"
	"testing"
)

// Автодополнение fish не должно отставать от диспетчера main().
func TestFishCompletionCoversCommands(t *testing.T) {
	for _, c := range []string{"serve", "migrate", "seed-demo-users", "connect", "sync", "config",
		"version", "completion", "help"} {
		if !strings.Contains(fishCompletionScript, "complete -c agctl-server -n '__fish_use_subcommand' -x -a "+c+" ") {
			t.Errorf("нет команды верхнего уровня %q", c)
		}
	}
	if strings.Contains(fishCompletionScript, "complete -c agctl ") {
		t.Error("строки автодополнения не для agctl-server")
	}
}
