package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/assets"
)

func TestNormalizeComponents(t *testing.T) {
	kit, ids, err := NormalizeComponents("", nil)
	if err != nil {
		t.Fatal(err)
	}
	if kit != "base" || strings.Join(ids, ",") != "core-workspace,core-instructions" {
		t.Fatalf("base defaults: kit=%q ids=%v", kit, ids)
	}

	kit, ids, err = NormalizeComponents("base", []string{"core-instructions", "core-workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if kit != "base" || strings.Join(ids, ",") != "core-workspace,core-instructions" {
		t.Fatalf("components must follow manifest order: kit=%q ids=%v", kit, ids)
	}
	if _, _, err := NormalizeComponents("base", []string{}); err == nil {
		t.Fatal("empty explicit selection must be rejected")
	}
	if _, _, err := NormalizeComponents("base", []string{"foreign-component"}); err == nil {
		t.Fatal("component outside the kit must be rejected")
	}
	if _, _, err := NormalizeComponents("unknown", nil); err == nil {
		t.Fatal("unknown kit must be rejected")
	}
}

// В ядре только универсальный комплект: предметные сценарии организаций
// (1С-разработка и пр.) приходят из их профилей.
func TestCoreKitsAreOrganizationFree(t *testing.T) {
	if len(Kits) != 1 || Kits[0].ID != "base" {
		ids := []string{}
		for _, k := range Kits {
			ids = append(ids, k.ID)
		}
		t.Fatalf("в ядре ожидается только base, есть: %v", ids)
	}
	if ValidTemplate("1c-vibe") {
		t.Fatal("комплект 1c-vibe вернулся в ядро — его место в профиле организации")
	}
}

func TestCopySelectedComponents(t *testing.T) {
	dst := t.TempDir()
	_, selected, err := NormalizeComponents("base", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopySelectedComponents(assets.RepoTemplate, "base", selected, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "README.md", ".gitignore", "skills/backlog/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("selected component file missing %s: %v", name, err)
		}
	}
}

func TestCopySubsetDoesNotInstallDeselectedComponents(t *testing.T) {
	dst := t.TempDir()
	if err := CopySelectedComponents(assets.RepoTemplate, "base", []string{"core-workspace"}, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("deselected core-instructions was installed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "skills/backlog/SKILL.md")); err != nil {
		t.Fatalf("selected core workspace component missing: %v", err)
	}
}

func TestWriteKitManifest(t *testing.T) {
	dst := t.TempDir()
	if err := WriteKitManifest(dst, "base", []string{"core-instructions", "core-workspace"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dst, "agctl-kit.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kit": "base"`, `"kitVersion": "1.0.0"`, `"core-workspace"`, `"core-instructions"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("manifest missing %s in:\n%s", want, raw)
		}
	}
	if err := WriteKitManifest(dst, "no-such-kit", nil); err == nil {
		t.Fatal("unknown kit must be rejected")
	}
	if err := WriteKitManifest(dst, "base", []string{}); err == nil {
		t.Fatal("empty explicit selection must be rejected")
	}
}
