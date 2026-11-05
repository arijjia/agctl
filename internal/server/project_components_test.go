package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedProjectFixture — imитация содержимого клона 1c-vibe в каталоге проекта.
func seedProjectFixture(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, body string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("skills/1c-vibe-demo/SKILL.md", "---\nname: 1C Vibe\ndescription: полный цикл 1С-разработки\n---\n")
	write("skills/backlog/SKILL.md", "# backlog\n")
	write("scripts/publish.py", "\"\"\"Publish branch as MR.\"\"\"\n")
	write("scripts/main.py", "#!/usr/bin/env python3\n")
	write("AGENTS.md", "# Правила 1С-контура\n")
}

func TestProjectComponentsAPI(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	user := e.newClient()
	e.register(user, "boris", "secret2")

	// Консоль компонентов открыта всем ролям, но только для СВОИХ воркспейсов:
	// чужого проекта у boris нет — 404 (это и есть граница доступа).
	if code, _ := e.do(user, "GET", "/api/projects/vibe/components", nil); code != 404 {
		t.Fatalf("user GET чужого = %d, want 404", code)
	}
	if code, _ := e.do(user, "PUT", "/api/projects/vibe/components", map[string]any{"disabled": []string{}}); code != 404 {
		t.Fatalf("user PUT чужого = %d, want 404", code)
	}
	// Несуществующий проект — 404, мусорное имя — 400 (маршрутизатор чистит
	// `../`, поэтому проверяем валидацию имени, а не обход каталогов).
	if code, _ := e.do(e.admin, "GET", "/api/projects/nope/components", nil); code != 404 {
		t.Fatalf("missing = %d, want 404", code)
	}
	if code, _ := e.do(e.admin, "GET", "/api/projects/.env/components", nil); code != 400 {
		t.Fatalf("invalid name = %d, want 400", code)
	}

	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "vibe"}); code != 201 {
		t.Fatalf("create %d %s", code, body)
	}
	dir := filepath.Join(e.home, "chats", "u1", "work", "vibe")
	seedProjectFixture(t, dir)

	code, body := e.do(e.admin, "GET", "/api/projects/vibe/components", nil)
	if code != 200 {
		t.Fatalf("GET %d %s", code, body)
	}
	var listing struct {
		Components []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
			Locked  bool   `json:"locked"`
			Title   string `json:"title"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatal(err)
	}
	byID := map[string]struct {
		Enabled bool
		Locked  bool
		Title   string
	}{}
	for _, comp := range listing.Components {
		byID[comp.ID] = struct {
			Enabled bool
			Locked  bool
			Title   string
		}{comp.Enabled, comp.Locked, comp.Title}
	}
	skill, ok := byID["skill:1c-vibe-demo"]
	if !ok || !skill.Enabled || skill.Title != "1C Vibe" {
		t.Fatalf("skill entry: %+v (body %s)", skill, body)
	}
	agents, ok := byID["docs:AGENTS.md"]
	if !ok || !agents.Locked {
		t.Fatalf("AGENTS entry must be locked: %+v", agents)
	}

	// Выключение скилла: уходит в stash, exclude получает правило.
	code, body = e.do(e.admin, "PUT", "/api/projects/vibe/components",
		map[string]any{"disabled": []string{"skill:1c-vibe-demo"}})
	if code != 200 {
		t.Fatalf("PUT %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "1c-vibe-demo")); !os.IsNotExist(err) {
		t.Fatalf("skill still in tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agctl", "stash", "skill", "1c-vibe-demo", "SKILL.md")); err != nil {
		t.Fatalf("skill not stashed: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), ".agctl/") {
		t.Fatalf("exclude: %q %v", exclude, err)
	}
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatal(err)
	}
	for _, comp := range listing.Components {
		if comp.ID == "skill:1c-vibe-demo" && comp.Enabled {
			t.Fatalf("catalog still enabled after PUT: %s", body)
		}
	}

	// Выбор сохраняется в KV (аудит/реаплай).
	raw, err := e.db.SettingGet("project-components-u1-vibe")
	if err != nil || !strings.Contains(raw, "skill:1c-vibe-demo") {
		t.Fatalf("state not persisted: %q %v", raw, err)
	}

	// Валидация: неизвестный и залоченный компоненты отклоняются.
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/components",
		map[string]any{"disabled": []string{"skill:missing"}}); code != 400 {
		t.Fatalf("unknown = %d, want 400", code)
	}
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/components",
		map[string]any{"disabled": []string{"docs:AGENTS.md"}}); code != 400 {
		t.Fatalf("locked = %d, want 400", code)
	}
	if code, body := e.do(e.admin, "PUT", "/api/projects/vibe/components",
		map[string]any{"disabled": []string{}, "extra": 1}); code != 400 {
		t.Fatalf("unknown field = %d %s, want 400", code, body)
	}

	// Включение обратно.
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/components",
		map[string]any{"disabled": []string{}}); code != 200 {
		t.Fatalf("re-enable = %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "1c-vibe-demo", "SKILL.md")); err != nil {
		t.Fatalf("skill not restored: %v", err)
	}
}

// Пресеты сценариев отдаются через /api/config (диалог создания воркспейса).
func TestConfigExposesScenarioPresets(t *testing.T) {
	e := newTestEnv(t, 3)
	code, body := e.do(e.newClient(), "GET", "/api/config", nil)
	if code != 200 {
		t.Fatalf("config %d %s", code, body)
	}
	if !strings.Contains(body, `"1c-vibe"`) || !strings.Contains(body, "github.com/arijjia/1c-vibe") {
		t.Fatalf("presets missing: %s", body)
	}
}
