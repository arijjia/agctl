package provision

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dsh-home как git-репозиторий: init с .gitignore, первый коммит без
// секретов, сессий и управляемых пресетов; повторный init ничего не ломает.
func TestDshHomeRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("нет git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig")) // без автора — коммитит agctl
	dir := filepath.Join(t.TempDir(), "ag_dsh")
	created, err := InitDshHomeRepo(dir)
	if err != nil || !created {
		t.Fatalf("init: created=%v err=%v", created, err)
	}
	if st := DshHomeRepoState(dir); !st.Repo || st.Commits || st.Branch != "main" {
		t.Fatalf("после init: %+v", st)
	}
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".credentials.yaml", "refs:\n  KEY: secret\n")
	write(".agctl/credentials.env", "JIRA_API_TOKEN=secret\n")
	write(".agent-presets/agctl-poisk/preset.yml", "name: agctl: Поиск\n")
	write(".agent-presets/mine/preset.yml", "name: мой\n")
	write("sessions/--workspace--/s1/session.v4.jsonl.zstd", "x")
	write("storages/workspace.json", "{}")
	write("profiles/web/package.json", "{}")
	write("profiles/web/node_modules/@x/y/index.js", "x")
	write("skills/demo/SKILL.md", "# demo\n")

	ok, err := CommitDshHome(dir, "agctl: начальная конфигурация")
	if err != nil || !ok {
		t.Fatalf("коммит: ok=%v err=%v", ok, err)
	}
	files, err := gitRun(dir, "ls-files")
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{".gitignore", ".agent-presets/mine/preset.yml", "profiles/web/package.json", "skills/demo/SKILL.md"} {
		if !strings.Contains(files, must) {
			t.Errorf("в коммите нет %s:\n%s", must, files)
		}
	}
	for _, never := range []string{".credentials.yaml", ".agctl/", "agctl-poisk", "sessions/", "storages/", "node_modules"} {
		if strings.Contains(files, never) {
			t.Errorf("в коммит попало %s:\n%s", never, files)
		}
	}
	if st := DshHomeRepoState(dir); !st.Commits || st.Dirty {
		t.Fatalf("после коммита: %+v", st)
	}
	// пустой набор изменений — не ошибка; повторный init не трогает .gitignore
	if ok, err := CommitDshHome(dir, "ещё"); ok || err != nil {
		t.Fatalf("пустой коммит: ok=%v err=%v", ok, err)
	}
	if created, err := InitDshHomeRepo(dir); created || err != nil {
		t.Fatalf("повторный init: created=%v err=%v", created, err)
	}
}
