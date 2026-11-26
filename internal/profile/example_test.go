package profile

import (
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/provision"
	"gopkg.in/yaml.v3"
	"io/fs"
)

// Пример профиля в ядре (profiles/example) — образец для организаций: он
// обязан загружаться текущим кодом, а его пакеты и мост — проходить те же
// проверки, что и у настоящего профиля. Иначе образец тихо устареет.
func TestExampleProfileIsValid(t *testing.T) {
	prof, err := LoadDir(filepath.Join("..", "..", "profiles", "example"))
	if err != nil {
		t.Fatalf("profiles/example не загружается: %v", err)
	}
	if prof.ID != "example" {
		t.Fatalf("id: %q", prof.ID)
	}
	pkgs, err := provision.CatalogPackages(prof.fsys)
	if err != nil || len(pkgs) == 0 {
		t.Fatalf("catalog: %v (%d пакетов)", err, len(pkgs))
	}
	skills, mcp := 0, 0
	for _, pkg := range pkgs {
		if len(provision.SkillPackageFiles(pkg)) > 0 {
			skills++
		}
		if _, ok, err := pkg.McpSpec(); err != nil {
			t.Fatalf("пакет %s: %v", pkg.ID, err)
		} else if ok {
			mcp++
		}
	}
	if skills == 0 || mcp == 0 {
		t.Fatalf("в примере должны быть и скилл, и MCP: skills=%d mcp=%d", skills, mcp)
	}
	raw, err := fs.ReadFile(prof.fsys, "local/bridge-tools.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var bridge struct {
		Tools []localbridge.Tool `yaml:"tools"`
	}
	if err := yaml.Unmarshal(raw, &bridge); err != nil {
		t.Fatal(err)
	}
	if err := localbridge.Validate(bridge.Tools); err != nil {
		t.Fatalf("bridge-tools.yaml: %v", err)
	}
}
