package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func skillTestFS() fstest.MapFS {
	return fstest.MapFS{
		"catalog/skill1/manifest.json":                           {Data: []byte(`{"id":"skill1","title":"Скилл"}`)},
		"catalog/skill1/files/.dsh/skills/skill1/SKILL.md":       {Data: []byte("hello")},
		"catalog/skill1/files/.dsh/skills/skill1/scripts/run.sh": {Data: []byte("echo hi")},
		"catalog/skill1/files/other/notes.txt":                   {Data: []byte("не скилл")},
	}
}

// Пакет каталога ставится в dsh-home: .dsh/skills/<имя>/… → skills/<имя>/…,
// файлы вне скиллов не переносятся.
func TestInstallSkillPackageToDshHome(t *testing.T) {
	fsys := skillTestFS()
	pkg, err := FindCatalogPackage(fsys, "skill1")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	if err := InstallSkillPackageToDshHome(home, fsys, pkg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "skills", "skill1", "SKILL.md"))
	if err != nil || string(raw) != "hello" {
		t.Fatalf("SKILL.md: %v %q", err, raw)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "skill1", "scripts", "run.sh")); err != nil {
		t.Fatalf("scripts/run.sh: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "other")); !os.IsNotExist(err) {
		t.Fatalf("не-скилловые файлы не должны ставиться в dsh-home: %v", err)
	}
	if got := SkillPackageFiles(pkg); len(got) != 2 {
		t.Fatalf("SkillPackageFiles: %v", got)
	}
}

// Удаление убирает файлы и пустые каталоги; изменённый файл защищён.
func TestRemoveSkillPackageFromDshHome(t *testing.T) {
	fsys := skillTestFS()
	pkg, _ := FindCatalogPackage(fsys, "skill1")
	home := t.TempDir()
	if err := InstallSkillPackageToDshHome(home, fsys, pkg); err != nil {
		t.Fatal(err)
	}
	// изменённый файл не удаляется молча
	if err := os.WriteFile(filepath.Join(home, "skills", "skill1", "SKILL.md"), []byte("правка"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSkillPackageFromDshHome(home, fsys, pkg); err == nil {
		t.Fatal("изменённый скилл-файл должен блокировать удаление")
	}
	// возвращаем исходник — удаляется целиком
	if err := os.WriteFile(filepath.Join(home, "skills", "skill1", "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveSkillPackageFromDshHome(home, fsys, pkg); err != nil {
		t.Fatal(err)
	}
	// скилл снят целиком; сам корень skills dsh-home остаётся (его наполняют
	// и другие компоненты — скилл моста и пресеты)
	if _, err := os.Stat(filepath.Join(home, "skills", "skill1")); !os.IsNotExist(err) {
		t.Fatalf("скилл должен быть снят: %v", err)
	}
}

// Обобщённый писатель пресетов: agctl-<id> с рядом MCP, повтор — идемпотентен,
// пустой состав снимает пресет; RemoveMcpPreset удаляет каталог.
func TestEnsureMcpPresetGeneric(t *testing.T) {
	home := t.TempDir()
	spec, ok := PlatformMcpServer("ddg")
	if !ok {
		t.Fatal("нет ddg в каталоге")
	}
	if err := EnsureMcpPreset(home, "agctl-demo", "agctl: agctl-demo", "Набор demo", []McpServerSpec{spec}, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".agent-presets", "agctl-demo")
	raw, err := os.ReadFile(filepath.Join(dir, "agent.cordis.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"serverName: ddg", "dsh-mcp-client"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("в пресете нет %q: %s", want, raw)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "preset.yml")); err != nil {
		t.Fatalf("preset.yml: %v", err)
	}
	if err := EnsureMcpPreset(home, "agctl-demo", "agctl: agctl-demo", "Набор demo", []McpServerSpec{spec}, nil); err != nil {
		t.Fatalf("повтор должен быть идемпотентным: %v", err)
	}
	if err := EnsureMcpPreset(home, "agctl-demo", "agctl: agctl-demo", "Набор demo", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("пустой состав должен снять пресет: %v", err)
	}
	if err := RemoveMcpPreset(home, "agctl-demo"); err != nil {
		t.Fatalf("RemoveMcpPreset на отсутствующем: %v", err)
	}
}
