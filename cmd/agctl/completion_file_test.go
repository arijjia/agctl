package main

import (
	"os"
	"testing"
)

// completions/agctl.fish в репо — то же, что печатает agctl completion fish:
// на него смотрит симлинк ~/.config/fish/completions/agctl.fish (make link).
// Расходится — make completions.
func TestCompletionFileInRepo(t *testing.T) {
	raw, err := os.ReadFile("../../completions/agctl.fish")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != fishCompletionScript {
		t.Fatal("completions/agctl.fish устарел — make completions")
	}
}
