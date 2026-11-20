//go:build server

package main

// mode_server_test.go — состав команд серверного варианта guest: ax-runner и
// init-chat-home, без моста.

import "testing"

func TestModeRowsServer(t *testing.T) {
	rows := modeRows()
	if len(rows) != 2 || rows[0].Name != "ax-runner" || rows[1].Name != "init-chat-home" {
		t.Fatalf("серверный guest: ждали ax-runner + init-chat-home, получили %v", rows)
	}
	if guestVariant != "server" {
		t.Fatalf("guestVariant = %q, ждали server", guestVariant)
	}
	for _, row := range help.Sections[0].Rows {
		if row.Name == "bridge" {
			t.Fatal("серверный guest: мост к хосту не должен входить в справку")
		}
	}
}
