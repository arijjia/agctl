package main

import (
	"os"
	"strings"
	"testing"
)

// completions/agctl-server.fish в репо — то же, что печатает agctl-server completion fish:
// на него смотрит симлинк ~/.config/fish/completions/agctl-server.fish (make link).
// Расходится — make completions.
func TestCompletionFileInRepo(t *testing.T) {
	raw, err := os.ReadFile("../../completions/agctl-server.fish")
	if err != nil {
		t.Fatal(err)
	}
	// На Windows git может отдать файл с CRLF — сравниваем содержимое.
	if strings.ReplaceAll(string(raw), "\r\n", "\n") != fishCompletionScript {
		t.Fatal("completions/agctl-server.fish устарел — make completions")
	}
}
