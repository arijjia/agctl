package server

// project_catalog_test.go — «возможности воркспейса»: платформенный каталог
// пакетов (установка/удаление, выбор при создании воркспейса) и системный
// промт воркспейса (AGENTS.md). Консоль доступна всем ролям для своих
// воркспейсов; истина о состоянии — файлы в рабочем дереве.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

// newOrgTestEnv — сервер с тестовым профилем организации: каталог пакетов
// и готовые сценарии приходят из профиля (testdata/org-profile).
func newOrgTestEnv(t *testing.T) *testEnv {
	t.Helper()
	prof, err := profile.LoadDir(filepath.Join("testdata", "org-profile"))
	if err != nil {
		t.Fatalf("profile: %v", err)
	}
	return newTestEnvWith(t, 3, func(c *Config) { c.Profile = prof })
}

// projectDirOf — host-каталог проекта u<id>/<name> в тестовом доме.
func projectDirOf(env *testEnv, userID int, name string) string {
	return filepath.Join(env.home, "chats", "u"+string(rune('0'+userID)), "work", name)
}

func TestCatalogListAndInstall(t *testing.T) {
	env := newOrgTestEnv(t)
	env.register(env.admin, "anna", "secret1")
	user := env.newClient()
	env.register(user, "boris", "secret2")

	// каталог платформенных пакетов виден всем авторизованным
	code, body := env.do(user, "GET", "/api/catalog", nil)
	if code != 200 || !contains(body, `"id":"presentations"`) {
		t.Fatalf("catalog: %d %s", code, body)
	}
	// несуществующий пакет в установке — 404
	if code, _ = env.do(user, "POST", "/api/projects/nope/catalog/presentations/install", nil); code != 404 {
		t.Fatalf("install в чужой/несуществующий: %d, ждём 404", code)
	}

	// создаём воркспейс сразу с пакетом
	code, body = env.do(user, "POST", "/api/projects", map[string]any{
		"name": "slides", "packages": []string{"presentations"},
	})
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	dir := projectDirOf(env, 2, "slides")
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations", "SKILL.md")); err != nil {
		t.Fatalf("скилл не установлен: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations", "templates", "template.pptx")); err != nil {
		t.Fatalf("шаблон не установлен: %v", err)
	}

	// каталог воркспейса показывает installed:true, компоненты — dshskill
	code, body = env.do(user, "GET", "/api/projects/slides/catalog", nil)
	if code != 200 || !contains(body, `"installed":true`) {
		t.Fatalf("ws catalog: %d %s", code, body)
	}
	code, body = env.do(user, "GET", "/api/projects/slides/components", nil)
	if code != 200 || !contains(body, `"dshskill:presentations"`) {
		t.Fatalf("components: %d %s", code, body)
	}

	// dshskill выключается и возвращается штатным механизмом stash
	code, body = env.do(user, "PUT", "/api/projects/slides/components",
		map[string]any{"disabled": []string{"dshskill:presentations"}})
	if code != 200 {
		t.Fatalf("disable dshskill: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("скилл остался в дереве: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agctl", "stash", "dshskill", "presentations", "SKILL.md")); err != nil {
		t.Fatalf("скилл не в stash: %v", err)
	}
	code, body = env.do(user, "PUT", "/api/projects/slides/components", map[string]any{"disabled": []string{}})
	if code != 200 {
		t.Fatalf("enable dshskill: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations", "SKILL.md")); err != nil {
		t.Fatalf("скилл не вернулся из stash: %v", err)
	}

	// удаление пакета (файлы не менялись) — всё уходит, пустые каталоги схлопываются
	code, body = env.do(user, "DELETE", "/api/projects/slides/catalog/presentations", nil)
	if code != 200 || !contains(body, `"installed":false`) {
		t.Fatalf("remove: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations")); !os.IsNotExist(err) {
		t.Fatalf("пакет не удалён: %v", err)
	}

	// повторная установка тем же пакетом — идемпотентное обновление
	if code, _ = env.do(user, "POST", "/api/projects/slides/catalog/presentations/install", nil); code != 200 {
		t.Fatalf("reinstall: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dsh", "skills", "presentations", "SKILL.md")); err != nil {
		t.Fatalf("reinstall не вернул файлы: %v", err)
	}
}

func TestProjectPromptCRUD(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	if code, _ := env.do(env.admin, "POST", "/api/projects", map[string]string{"name": "notes"}); code != 201 {
		t.Fatal("create")
	}

	// пусто → пустой промт
	code, body := env.do(env.admin, "GET", "/api/projects/notes/prompt", nil)
	if code != 200 || !contains(body, `"prompt":""`) {
		t.Fatalf("get empty: %d %s", code, body)
	}
	prompt := "# Правила воркспейса\nОтчёты оформлять по ГОСТ."
	if code, body = env.do(env.admin, "PUT", "/api/projects/notes/prompt", map[string]string{"prompt": prompt}); code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	dir := projectDirOf(env, 1, "notes")
	raw, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil || string(raw) != prompt {
		t.Fatalf("AGENTS.md: %v %q", err, string(raw))
	}
	code, body = env.do(env.admin, "GET", "/api/projects/notes/prompt", nil)
	if code != 200 || !contains(body, "по ГОСТ") {
		t.Fatalf("get: %d %s", code, body)
	}
	// пустой промт = удалить файл (воркспейс без промта)
	if code, _ = env.do(env.admin, "PUT", "/api/projects/notes/prompt", map[string]string{"prompt": "  "}); code != 200 {
		t.Fatal("put empty")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("пустой промт не удалил файл: %v", err)
	}
	// мусорный JSON / слишком большой промт — 400
	if code, _ = env.do(env.admin, "PUT", "/api/projects/notes/prompt", map[string]int{"wrong": 1}); code != 400 {
		t.Fatalf("invalid body: %d, ждём 400", code)
	}
}

func TestProjectCreateUnknownPackage(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	code, body := env.do(env.admin, "POST", "/api/projects", map[string]any{
		"name": "x", "packages": []string{"no-such-package"},
	})
	if code != 400 {
		t.Fatalf("create с неизвестным пакетом: %d %s, ждём 400", code, body)
	}
	// воркспейс не должен был создаться
	if code, _ = env.do(env.admin, "GET", "/api/projects/x/components", nil); code != 404 {
		t.Fatalf("воркспейс создан вопреки ошибке пакета: %d", code)
	}
}

// TestCatalogCreateResponse — при создании с пакетами ответ содержит
// packagesFailed (пустой) и descriptor проекта; JSON-структура стабильна.
func TestCatalogCreateResponse(t *testing.T) {
	env := newOrgTestEnv(t)
	env.register(env.admin, "anna", "secret1")
	code, body := env.do(env.admin, "POST", "/api/projects", map[string]any{
		"name": "resp", "packages": []string{"presentations"},
	})
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	var resp struct {
		Project struct {
			Name string `json:"name"`
		} `json:"project"`
		PackagesFailed []string `json:"packagesFailed"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v %s", err, body)
	}
	if resp.Project.Name != "resp" || len(resp.PackagesFailed) != 0 {
		t.Fatalf("resp: %+v", resp)
	}
}

// TestProjectDeleteAPI — DELETE /api/projects/{name}: свой воркспейс
// удаляется с диска (идемпотентно), чужой — 404.
func TestProjectDeleteAPI(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	user := env.newClient()
	env.register(user, "boris", "secret2")
	if code, _ := env.do(user, "POST", "/api/projects", map[string]string{"name": "gone"}); code != 201 {
		t.Fatal("create")
	}
	dir := projectDirOf(env, 2, "gone")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir: %v", err)
	}
	// чужой пользователь не удаляет чужое: у админа такого воркспейса нет —
	// идемпотентный успех, но каталог boris не тронут
	if code, _ := env.do(env.admin, "DELETE", "/api/projects/gone", nil); code != 200 {
		t.Fatalf("чужой delete: %d", code)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("чужой удалил каталог: %v", err)
	}
	// свой: удаление + идемпотентный повтор
	if code, body := env.do(user, "DELETE", "/api/projects/gone", nil); code != 200 || !contains(body, `"deleted":true`) {
		t.Fatalf("delete: %d %s", code, body)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("каталог не удалён: %v", err)
	}
	if code, _ := env.do(user, "DELETE", "/api/projects/gone", nil); code != 200 {
		t.Fatalf("re-delete: %d, ждём 200 (идемпотентно)", code)
	}
	// в списке проектов его больше нет
	if code, body := env.do(user, "GET", "/api/projects", nil); code != 200 || contains(body, "gone") {
		t.Fatalf("list: %d %s", code, body)
	}
}

// TestNeutralProfileHasNoOrgContent — нейтральный профиль (и любой профиль
// без catalog/ и workspace.scenarios) не показывает чужих пакетов и
// сценариев: содержимое организаций живёт только в их профилях.
func TestNeutralProfileHasNoOrgContent(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	if code, body := env.do(env.admin, "GET", "/api/catalog", nil); code != 200 || !contains(body, `"packages":[]`) {
		t.Fatalf("catalog: %d %s", code, body)
	}
	code, body := env.do(env.admin, "GET", "/api/config", nil)
	if code != 200 || !contains(body, `"presets":[]`) {
		t.Fatalf("config: %d %s", code, body)
	}
	if code, body := env.do(env.admin, "POST", "/api/projects", map[string]any{
		"name": "p", "packages": []string{"presentations"},
	}); code != 400 {
		t.Fatalf("пакет вне каталога профиля: %d %s, ждём 400", code, body)
	}
}
