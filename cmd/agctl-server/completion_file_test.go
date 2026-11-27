package main

import (
	"os"
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
	if string(raw) != fishCompletionScript {
		t.Fatal("completions/agctl-server.fish устарел — make completions")
	}
}
