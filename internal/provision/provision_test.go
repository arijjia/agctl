package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/backlog"
)

func testProvider() Provider {
	return Provider{BaseURL: "https://litellm.example/v1", Model: "qwen-test", APIKey: "sk-test-token"}
}

// TestProvisionFull — полный цикл: bare+клон+шаблон+dsh-home, autosave.
func TestProvisionFull(t *testing.T) {
	home := t.TempDir()
	l := Paths(home, "anna", "w-abcd1234ef56", "Моя Работа")
	p := testProvider()

	if err := Provision(l, p, "base", assets.RepoTemplate, assets.DshSkel); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// bare: HEAD → main, есть коммит
	bare, err := git.PlainOpen(l.Repo)
	if err != nil {
		t.Fatalf("bare open: %v", err)
	}
	head, err := bare.Reference(plumbing.HEAD, true)
	if err != nil {
		t.Fatalf("bare HEAD resolve: %v", err)
	}
	if head.Name() != plumbing.NewBranchReferenceName("main") {
		t.Fatalf("bare HEAD = %s, ждали main", head.Name())
	}

	// рабочий клон: шаблон на месте, дерево чистое
	for _, f := range []string{"AGENTS.md", "README.md", "skills/backlog/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(l.Work, f)); err != nil {
			t.Errorf("нет файла шаблона %s: %v", f, err)
		}
	}
	repo, err := git.PlainOpen(l.Work)
	if err != nil {
		t.Fatalf("work open: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	st, err := wt.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.IsClean() {
		t.Fatalf("рабочее дерево не чистое после provision: %v", st)
	}

	// dsh-home: скелет + настройки провайдера + токен (0600)
	creds := filepath.Join(l.DshHome, ".credentials.yaml")
	b, err := os.ReadFile(creds)
	if err != nil {
		t.Fatalf("нет .credentials.yaml: %v", err)
	}
	if !strings.Contains(string(b), "sk-test-token") || !strings.Contains(string(b), "AGCTL_LLM_API_KEY") {
		t.Fatalf(".credentials.yaml без токена/ref: %s", b)
	}
	if fi, err := os.Stat(creds); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf(".credentials.yaml должен быть 0600")
	}
	settings, err := os.ReadFile(filepath.Join(l.DshHome, "settings.yaml"))
	if err != nil {
		t.Fatalf("нет settings.yaml: %v", err)
	}
	for _, want := range []string{"litellm.example/v1", "qwen-test", "apiKeyEnv: AGCTL_LLM_API_KEY"} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings.yaml без %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(l.DshHome, "profiles", "web", "package.json")); err != nil {
		t.Errorf("нет скелета профиля web: %v", err)
	}

	// autosave: новое изменение → commit+push в bare
	if err := os.WriteFile(filepath.Join(l.Work, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := AutosaveOnce(l.Work)
	if err != nil {
		t.Fatalf("autosave: %v", err)
	}
	if !changed {
		t.Fatal("autosave не закоммитил изменение")
	}
	changed, err = AutosaveOnce(l.Work)
	if err != nil || changed {
		t.Fatalf("повторный autosave должен быть no-op: changed=%v err=%v", changed, err)
	}
	// в bare появился второй коммит
	mainRef, err := bare.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	iter, err := bare.Log(&git.LogOptions{From: mainRef.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = iter.ForEach(func(*object.Commit) error { n++; return nil })
	if n < 2 {
		t.Fatalf("в bare %d коммитов, ждали ≥2", n)
	}
}

// TestBacklogFlow — CLI поверх свежего репо: check/new/done.
func TestBacklogFlow(t *testing.T) {
	home := t.TempDir()
	l := Paths(home, "anna", "w-abcd1234ef56", "ws")
	if err := Provision(l, testProvider(), "base", assets.RepoTemplate, assets.DshSkel); err != nil {
		t.Fatalf("provision: %v", err)
	}
	root := l.Work
	// --root — сам каталог backlog/ (как возвращает Root()); cwd-поиск в CLI тот же.
	backlogDir := filepath.Join(root, "backlog")
	run := func(args ...string) int {
		return backlog.Main(append([]string{"--root", backlogDir}, args...), os.Stdout, os.Stderr)
	}
	if code := run("check"); code != 0 {
		t.Fatalf("backlog check = %d, ждали 0", code)
	}
	if code := run("new", "--title", "Починить велосипед", "--priority", "P1"); code != 0 {
		t.Fatalf("backlog new = %d", code)
	}
	// карточка появилась
	entries, _ := os.ReadDir(filepath.Join(root, "backlog", "open"))
	if len(entries) != 2 {
		t.Fatalf("в open %d карточек, ждали 2", len(entries))
	}
	// done onboarding
	if code := run("done", "onboarding"); code != 0 {
		t.Fatalf("backlog done = %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "backlog", "done", "2026-09-28-onboarding.md")); err != nil {
		t.Fatalf("карточка не переехала в done/: %v", err)
	}
	if code := run("check"); code != 0 {
		t.Fatalf("check после done = %d", code)
	}
}

// object нужен только для подсчёта коммитов в тесте.
