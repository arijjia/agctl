package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestDZ2EnsurePermissionDefaults(t *testing.T) {
	home := t.TempDir()
	patch := filepath.Join(home, "profiles", "web", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(patch), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch, []byte("- id: keep\n  name: \"@deepseek-ai/dsh-agent-default-model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Роль user: запись с defaultPreset появляется и идемпотентна.
	for i := 0; i < 2; i++ {
		if err := EnsurePermissionDefaults(home, true); err != nil {
			t.Fatalf("ensure #%d: %v", i+1, err)
		}
	}
	raw, _ := os.ReadFile(patch)
	if !strings.Contains(string(raw), "dsh-permission-presets") || !strings.Contains(string(raw), "id: permission") || !strings.Contains(string(raw), "danger-full-access") {
		t.Fatalf("нет пресета full access:\n%s", raw)
	}
	if strings.Count(string(raw), "dsh-permission-presets") != 1 {
		t.Fatalf("дубликат записи пресета:\n%s", raw)
	}
	if !strings.Contains(string(raw), "dsh-agent-default-model") {
		t.Fatalf("существующая запись потеряна:\n%s", raw)
	}
	// Роль снята: запись удаляется, остальное сохраняется.
	if err := EnsurePermissionDefaults(home, false); err != nil {
		t.Fatalf("ensure(false): %v", err)
	}
	raw, _ = os.ReadFile(patch)
	if strings.Contains(string(raw), "dsh-permission-presets") {
		t.Fatalf("запись не удалена после снятия роли user:\n%s", raw)
	}
	if !strings.Contains(string(raw), "dsh-agent-default-model") {
		t.Fatalf("потеряна соседняя запись:\n%s", raw)
	}
}
