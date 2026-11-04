package provision

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// seedVibeProject — скелет проекта в духе клона 1c-vibe: скилл, инструменты,
// демо-данные, инструкции и git-репа (как делает CloneProject).
func seedVibeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/1c-vibe-demo/SKILL.md", "---\nname: 1C Vibe\n---\n# Цикл Jira → ветка → MR\n\ndescription: полный цикл 1С-разработки\n")
	write("skills/backlog/SKILL.md", "# backlog\n")
	write("scripts/publish.py", "#!/usr/bin/env python3\n\"\"\"Publish branch as MR.\"\"\"\n")
	write("scripts/lint.py", "#!/usr/bin/env python3\n'''BSL lint runner.'''\n")
	write("scripts/main.py", "#!/usr/bin/env python3\n")
	write("scripts/demo-project/Configuration.xml", "<Configuration/>\n")
	write("AGENTS.md", "# Правила 1С-контура\n")
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "t", Email: "t@example"}
	if _, err := wt.Commit("init", &git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: true}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func findComponent(t *testing.T, components []ProjectComponent, id string) ProjectComponent {
	t.Helper()
	for _, comp := range components {
		if comp.ID == id {
			return comp
		}
	}
	t.Fatalf("компонент %s не найден в каталоге (%d записей)", id, len(components))
	return ProjectComponent{}
}

func TestScanProjectComponentsCatalog(t *testing.T) {
	dir := seedVibeProject(t)
	components := ScanProjectComponents(dir)

	skill := findComponent(t, components, "skill:1c-vibe-demo")
	if !skill.Enabled || skill.Title != "1C Vibe" || skill.Description != "полный цикл 1С-разработки" {
		t.Fatalf("skill meta: %+v", skill)
	}
	if comp := findComponent(t, components, "tool:publish.py"); comp.Title != "publish" || comp.Description != "Publish branch as MR." {
		t.Fatalf("tool meta: %+v", comp)
	}
	if comp := findComponent(t, components, "tool:lint.py"); comp.Description != "BSL lint runner." {
		t.Fatalf("lint doc: %+v", comp)
	}
	if comp := findComponent(t, components, "data:demo-project"); !comp.Enabled {
		t.Fatalf("demo data: %+v", comp)
	}
	agents := findComponent(t, components, "docs:AGENTS.md")
	if !agents.Locked {
		t.Fatalf("AGENTS.md должен быть locked: %+v", agents)
	}
	if _, ok := func() (ProjectComponent, bool) {
		for _, comp := range components {
			if comp.ID == "tool:tests" {
				return comp, true
			}
		}
		return ProjectComponent{}, false
	}(); ok {
		t.Fatal("scripts/tests не должен попадать в каталог")
	}
}

func TestApplyProjectComponentsToggleRoundtrip(t *testing.T) {
	dir := seedVibeProject(t)

	// 1. Выключить скилл и инструмент.
	if err := ApplyProjectComponents(dir, []string{"skill:1c-vibe-demo", "tool:lint.py"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "1c-vibe-demo")); !os.IsNotExist(err) {
		t.Fatalf("скилл остался в дереве: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stashRel, "skill", "1c-vibe-demo", "SKILL.md")); err != nil {
		t.Fatalf("скилл не в stash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "lint.py")); !os.IsNotExist(err) {
		t.Fatalf("инструмент остался в дереве: %v", err)
	}
	// exclude-файл получил правило stash.
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !bytes.Contains(exclude, []byte(".agctl/")) {
		t.Fatalf("exclude: %q %v", exclude, err)
	}
	// Каталог показывает компонент выключенным.
	if comp := findComponent(t, ScanProjectComponents(dir), "skill:1c-vibe-demo"); comp.Enabled {
		t.Fatalf("каталог должен показывать выключенным: %+v", comp)
	}
	// git не видит stash как untracked при autosave-исключении: go-git прячет
	// .git из worktree-FS (info/exclude для него не читается), поэтому статус
	// проверяем ровно так, как его считает AutosaveLocalProject.
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	wt.Excludes = append(wt.Excludes, StashIgnorePattern)
	status, err := wt.Status()
	if err != nil {
		t.Fatal(err)
	}
	for path := range status {
		if strings.Contains(path, ".agctl") {
			t.Fatalf("stash попал в git-status: %s", path)
		}
	}

	// 2. Повторное применение того же состояния — идемпотентно.
	if err := ApplyProjectComponents(dir, []string{"skill:1c-vibe-demo", "tool:lint.py"}); err != nil {
		t.Fatalf("idempotent disable: %v", err)
	}

	// 3. Включить обратно.
	if err := ApplyProjectComponents(dir, []string{}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "1c-vibe-demo", "SKILL.md")); err != nil {
		t.Fatalf("скилл не вернулся: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "scripts", "lint.py")); err != nil {
		t.Fatalf("инструмент не вернулся: %v", err)
	}
	if comp := findComponent(t, ScanProjectComponents(dir), "skill:1c-vibe-demo"); !comp.Enabled || comp.Title != "1C Vibe" {
		t.Fatalf("метаданные потеряны после stash: %+v", comp)
	}
}

func TestApplyProjectComponentsRejectsUnknownAndLocked(t *testing.T) {
	dir := seedVibeProject(t)
	components := ScanProjectComponents(dir)
	if err := ValidateComponentSelection(components, []string{"skill:no-such"}); err == nil {
		t.Fatal("неизвестный компонент должен отклоняться")
	}
	if err := ValidateComponentSelection(components, []string{"docs:AGENTS.md"}); err == nil {
		t.Fatal("залоченный компонент должен отклоняться")
	}
	if err := ValidateComponentSelection(components, []string{"skill:1c-vibe-demo", "skill:1c-vibe-demo"}); err == nil {
		t.Fatal("дубликат должен отклоняться")
	}
}

func TestComponentsStateEncodeDecode(t *testing.T) {
	state := ProjectComponentsState{Schema: 1, Disabled: []string{"skill:x"}}
	raw, err := EncodeComponentsState(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := DecodeComponentsState(raw); len(got.Disabled) != 1 || got.Disabled[0] != "skill:x" {
		t.Fatalf("roundtrip: %+v", got)
	}
	// Битая/пустая запись = ничего не выключено, schema нормализована.
	if got := DecodeComponentsState(""); got.Schema != 1 || len(got.Disabled) != 0 {
		t.Fatalf("empty: %+v", got)
	}
	if got := DecodeComponentsState("{broken"); got.Schema != 1 {
		t.Fatalf("broken: %+v", got)
	}
}
