package main

import (
	"bytes"
	"strings"
	"testing"
)

// Справка agctl перечисляет команды разработчика и не упоминает серверные.
func TestUsageListsDeveloperCommands(t *testing.T) {
	var buf bytes.Buffer
	help.Render(&buf, false)
	out := buf.String()
	for _, name := range []string{"dev init", "dev up", "backlog", "completion fish", "version", "help", "AGCTL_DEV_HOME"} {
		if !strings.Contains(out, name) {
			t.Errorf("справка не содержит %q", name)
		}
	}
	for _, row := range help.CommandRows() {
		switch row.Name {
		case "serve", "migrate", "ax-runner", "init-chat-home", "connect", "sync", "bridge":
			t.Errorf("серверная команда %q в справке agctl", row.Name)
		}
	}
	for _, name := range []string{"ax-runner", "init-chat-home", "seed-demo-users"} {
		if strings.Contains(out, name) {
			t.Errorf("серверная команда %q в справке agctl", name)
		}
	}
}
