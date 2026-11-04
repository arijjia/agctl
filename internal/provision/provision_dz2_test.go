package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDZ2EnsurePermissionDefaults — D7: роль user получает defaultPreset
// danger-full-access в cordis.patch.yml профиля; снятие роли удаляет запись,
// соседние записи переживают оба перехода; повторный ensure идемпотентен.
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

// TestDZ2EnsurePermissionDefaultsNoPatchFile — developer без прежней
// user-настройки: файл не создаётся (dev-workspace остаётся девственно чист).
func TestDZ2EnsurePermissionDefaultsNoPatchFile(t *testing.T) {
	home := t.TempDir()
	if err := EnsurePermissionDefaults(home, false); err != nil {
		t.Fatalf("ensure(false) без файла: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "profiles", "web", "cordis.patch.yml")); !os.IsNotExist(err) {
		t.Fatalf("файл не должен создаваться для developer без user-настройки: %v", err)
	}
}
