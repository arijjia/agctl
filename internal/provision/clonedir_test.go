package provision

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Клон dsh-home: файлы, права, вложенные каталоги и симлинки на месте;
// в существующий каталог клон не пишет. Запасной путь (copyTree) — тоже.
func TestCloneDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), "dsh-home")
	mk := func(rel, body string, perm os.FileMode) {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), perm); err != nil {
			t.Fatal(err)
		}
	}
	mk(".credentials.yaml", "refs: {}\n", 0o600)
	mk("sessions/--workspace--/s1/session.v4.jsonl.zstd", "data", 0o600)
	mk("skills/demo/SKILL.md", "# demo\n", 0o644)
	if err := os.Symlink("skills/demo", filepath.Join(src, "demo-link")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	check := func(dst string) {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dst, "sessions", "--workspace--", "s1", "session.v4.jsonl.zstd"))
		if err != nil || string(raw) != "data" {
			t.Fatalf("сессия в клоне: %q %v", raw, err)
		}
		if runtime.GOOS != "windows" {
			fi, err := os.Stat(filepath.Join(dst, ".credentials.yaml"))
			if err != nil || fi.Mode().Perm() != 0o600 {
				t.Fatalf("права секрета в клоне: %v %v", fi, err)
			}
			if l, err := os.Readlink(filepath.Join(dst, "demo-link")); err != nil || l != "skills/demo" {
				t.Fatalf("симлинк в клоне: %q %v", l, err)
			}
		}
	}

	dst := filepath.Join(t.TempDir(), "nested", "clone")
	how, err := CloneDir(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("способ клона: %s", how)
	check(dst)
	if _, err := CloneDir(src, dst); err == nil {
		t.Fatal("клон поверх существующего каталога должен отказать")
	}

	fallback := filepath.Join(t.TempDir(), "copy")
	if err := copyTree(src, fallback); err != nil {
		t.Fatal(err)
	}
	check(fallback)
}
