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

	kit, ids, err = NormalizeComponents("1c-vibe", []string{"1c-demo-project", "core-workspace", "1c-tooling"})
	if err != nil {
		t.Fatal(err)
	}
	if kit != "1c-vibe" || strings.Join(ids, ",") != "core-workspace,1c-tooling,1c-demo-project" {
		t.Fatalf("components must follow manifest order: kit=%q ids=%v", kit, ids)
	}
	if _, _, err := NormalizeComponents("1c-vibe", []string{}); err == nil {
		t.Fatal("empty explicit selection must be rejected")
	}
	if _, _, err := NormalizeComponents("base", []string{"1c-demo-project"}); err == nil {
		t.Fatal("component from another kit must be rejected")
	}
	if _, _, err := NormalizeComponents("unknown", nil); err == nil {
		t.Fatal("unknown kit must be rejected")
	}
}

func TestCopySelectedComponentsAndOverlay(t *testing.T) {
	dst := t.TempDir()
	_, selected, err := NormalizeComponents("1c-vibe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := CopySelectedComponents(assets.RepoTemplate, "1c-vibe", selected, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"AGENTS.md", "README.md", ".gitignore", "backlog/open/2026-09-28-1c-vibe-onboarding.md",
		"scripts/jira_client.py", "scripts/publish.py", "scripts/tests/test_clients.py",
		"scripts/demo-project/Configuration.xml", "skills/1c-vibe-demo/SKILL.md",
	} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("selected component file missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dst, "scripts/.pytest_cache")); !os.IsNotExist(err) {
		t.Fatalf("generated pytest cache must not be packaged, stat err=%v", err)
	}
	agents, err := os.ReadFile(filepath.Join(dst, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "рабочее место 1С-разработки") {
		t.Fatal("kit manifest overlay order did not select the 1C instructions")
	}
}

func TestCopySubsetDoesNotInstallDeselectedComponents(t *testing.T) {
	dst := t.TempDir()
	if err := CopySelectedComponents(assets.RepoTemplate, "1c-vibe", []string{"core-workspace", "core-instructions"}, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "scripts/jira_client.py")); !os.IsNotExist(err) {
		t.Fatalf("deselected 1C tooling was installed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "skills/backlog/SKILL.md")); err != nil {
		t.Fatalf("selected core workspace component missing: %v", err)
	}
}

func TestWriteKitManifest(t *testing.T) {
	dst := t.TempDir()
	if err := WriteKitManifest(dst, "1c-vibe", []string{"1c-demo-project", "core-workspace"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dst, "agctl-kit.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kit": "1c-vibe"`, `"kitVersion": "1.0.0"`, `"core-workspace"`, `"1c-demo-project"`} {
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
