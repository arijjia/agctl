package provision

import "testing"

func TestDZ2ParseModelsEdges(t *testing.T) {
	got := ParseModels("m1\nm2,m1\n  m3  \n\n", "")
	if len(got) != 3 || got[0] != "m1" || got[2] != "m3" {
		t.Fatalf("ParseModels = %v, ждали [m1 m2 m3] без дубликатов и пустых", got)
	}
	if got := ParseModels("", "fb"); len(got) != 1 || got[0] != "fb" {
		t.Fatalf("fallback = %v", got)
	}
	if got := ParseModels("   ", ""); got != nil {
		t.Fatalf("пусто + без fallback = %v, ждали nil", got)
	}
}

func TestDZ2NewIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id, _ := NewID()
		if id == "" || seen[id] {
			t.Fatalf("NewID пуст или дублируется: %q", id)
		}
		seen[id] = true
	}
}
