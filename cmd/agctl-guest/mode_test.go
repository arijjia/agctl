//go:build !server

package main

// mode_test.go — состав команд локального варианта guest: только мост.

import "testing"

func TestModeRowsLocal(t *testing.T) {
	rows := modeRows()
	if len(rows) != 1 || rows[0].Name != "bridge" {
		t.Fatalf("локальный guest: ждали только bridge, получили %v", rows)
	}
	if guestVariant != "local" {
		t.Fatalf("guestVariant = %q, ждали local", guestVariant)
	}
}

func TestServerCommandsRefuseInLocalBuild(t *testing.T) {
	// Стабы должны существовать (сборка проходит), команды в help не входят.
	for _, row := range help.Sections[0].Rows {
		switch row.Name {
		case "ax-runner", "init-chat-home":
			t.Fatalf("локальный guest: лишняя команда %q в справке", row.Name)
		}
	}
}
