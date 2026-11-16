package main

import (
	"bytes"
	"strings"
	"testing"
)

// Справка agctl-server перечисляет серверные команды и не упоминает
// локальный режим.
func TestUsageListsServerCommands(t *testing.T) {
	var buf bytes.Buffer
	help.Render(&buf, false)
	out := buf.String()
	for _, name := range []string{
		"serve", "migrate", "seed-demo-users", "connect", "sync", "config",
		"completion fish", "version", "help", "AGCTL_HOME", "AGCTL_CONFIG",
	} {
		if !strings.Contains(out, name) {
			t.Errorf("справка не содержит %q", name)
		}
	}
	if strings.Contains(out, "dev up") {
		t.Error("локальный режим в справке agctl-server")
	}
}
