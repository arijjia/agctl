package server

// local_presets_test.go — пресеты лаунчера: CRUD с ревизиями (409 на
// устаревшую), материализация в dsh-home мест (MCP-пресет agctl-<id>),
// снятие с мест при изменении и удалении, дефолтная папка нового места
// и системный диалог выбора папки (подмена в тестах).

import (
	"gopkg.in/yaml.v3"

	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Полный цикл: создать место и пресет → файлы в dsh-home обоих мест →
// убрать место из пресета → удалить пресет → файлов нет.
func TestLocalPresetsCRUDAndMaterialization(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	owner := env.srv.local.ownerID
	dirs := mkdirs(t, t.TempDir(), "alpha")
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "Демо", "id": "demo", "mounts": []map[string]string{{"host": dirs[0]}},
	}); code != http.StatusCreated {
		t.Fatalf("место: %d %s", code, body)
	}

	// каталог MCP в ответе списка — для конструктора галочками
	code, body := env.do(c, "GET", "/api/local/ui/presets", nil)
	list := decode[struct {
		Presets []localPreset    `json:"presets"`
		MCP     []map[string]any `json:"mcp"`
		Skills  []map[string]any `json:"skills"`
		Places  []map[string]any `json:"places"`
	}](t, body)
	if code != 200 || len(list.Presets) != 0 || len(list.MCP) == 0 || len(list.Places) != 2 {
		t.Fatalf("список до создания: %d %s", code, body)
	}

	code, body = env.do(c, "POST", "/api/local/ui/presets", map[string]any{
		"name": "web-nabor", "description": "поиск и браузер", "mcp": []string{"ddg", "playwright"}, "places": []string{"main", "demo"},
	})
	created := decode[localPreset](t, body)
	if code != http.StatusCreated || created.ID != "web-nabor" || created.Revision != 1 {
		t.Fatalf("создание пресета: %d %s", code, body)
	}

	presetFile := func(dshHome string) string {
		raw, err := os.ReadFile(filepath.Join(dshHome, ".agent-presets", "agctl-"+created.ID, "agent.cordis.yml"))
		if err != nil {
			t.Fatalf("пресет в dsh-home %s: %v", dshHome, err)
		}
		return string(raw)
	}
	mainHome := env.srv.dshHomeFor(userKey(owner))
	demoHome := env.srv.dshHomeFor(chatKey{uid: owner, slot: 1})
	for _, home := range []string{mainHome, demoHome} {
		raw := presetFile(home)
		if !strings.Contains(raw, "serverName: ddg") {
			t.Fatalf("пресет без ddg: %s", raw)
		}
	}

	// дубль id — 409; неизвестный MCP — 400; неизвестное место — 400
	if code, _ := env.do(c, "POST", "/api/local/ui/presets", map[string]any{"name": "web-nabor", "mcp": []string{"ddg"}, "places": []string{"main"}}); code != http.StatusConflict {
		t.Fatalf("дубль id: %d", code)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/presets", map[string]any{"name": "Другое", "mcp": []string{"нет"}}); code != http.StatusBadRequest {
		t.Fatalf("неизвестный mcp: %d", code)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/presets", map[string]any{"name": "Другое", "places": []string{"нет-места"}}); code != http.StatusBadRequest {
		t.Fatalf("неизвестное место: %d", code)
	}

	// устаревшая ревизия — 409
	if code, _ := env.do(c, "PUT", "/api/local/ui/presets/"+created.ID, map[string]any{
		"name": "Веб-набор", "revision": int64(0), "mcp": []string{"ddg"}, "places": []string{"main"},
	}); code != http.StatusConflict {
		t.Fatalf("устаревшая ревизия: %d", code)
	}

	// обновление: пресет остаётся только на demo — с основного снят
	code, body = env.do(c, "PUT", "/api/local/ui/presets/"+created.ID, map[string]any{
		"name": "Веб-набор", "revision": created.Revision, "mcp": []string{"ddg"}, "places": []string{"demo"},
	})
	updated := decode[localPreset](t, body)
	if code != 200 || updated.Revision != 2 || len(updated.Places) != 1 {
		t.Fatalf("обновление: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(mainHome, ".agent-presets", "agctl-"+created.ID)); !os.IsNotExist(err) {
		t.Fatalf("с основного места пресет должен быть снят: %v", err)
	}
	presetFile(demoHome) // на demo живёт

	// удаление — файлы сняты, запись исчезла
	if code, _ := env.do(c, "DELETE", "/api/local/ui/presets/"+created.ID, nil); code != 200 {
		t.Fatalf("удаление: %d", code)
	}
	if _, err := os.Stat(filepath.Join(demoHome, ".agent-presets", "agctl-"+created.ID)); !os.IsNotExist(err) {
		t.Fatalf("после удаления пресет должен быть снят с мест: %v", err)
	}
	if code, body := env.do(c, "GET", "/api/local/ui/presets", nil); code != 200 || strings.Contains(body, `"id":"`+created.ID+`"`) {
		t.Fatalf("удалённый пресет в списке: %d %s", code, body)
	}
}

// Проверка валидации состава: скиллов нет в пустом каталоге профиля теста — 400.
func TestLocalPresetRejectsUnknownSkill(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	mainHome := env.srv.dshHomeFor(userKey(env.srv.local.ownerID))
	// Кириллическое имя без MCP: id транслитерируется (раньше выходило
	// «folder»), а каталог пресета создаётся и без MCP-рядов (раньше пресет
	// только со скиллами в dsh не попадал вовсе).
	ruCode, ruBody := env.do(c, "POST", "/api/local/ui/presets", map[string]any{"name": "Аналитика: отчёты", "places": []string{"main"}})
	ru := decode[localPreset](t, ruBody)
	if ruCode != http.StatusCreated || ru.ID != "analitika-otchety" {
		t.Fatalf("кириллический пресет: %d %s", ruCode, ruBody)
	}
	meta, err := os.ReadFile(filepath.Join(mainHome, ".agent-presets", "agctl-"+ru.ID, "preset.yml"))
	if err != nil {
		t.Fatalf("пресет без MCP не материализован: %v", err)
	}
	var parsed struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(meta, &parsed); err != nil || parsed.Name != "Аналитика: отчёты" {
		t.Fatalf("preset.yml: %v %q\n%s", err, parsed.Name, meta)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/presets", map[string]any{"name": "X", "skills": []string{"нет-пакета"}}); code != http.StatusBadRequest {
		t.Fatalf("неизвестный скилл: %d", code)
	}
}

// Новое место без папок получает дефолтную <папка проектов>/<id> → /workspace.
func TestLocalPlaceCreateDefaultMount(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	root := env.srv.localWorkDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r // normalizeMounts возвращает путь с разрешёнными симлинками
	}
	want := filepath.Join(root, "auto")
	code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": "Авто", "id": "auto"})
	pv := decode[placeView](t, body)
	if code != http.StatusCreated || len(pv.Mounts) != 1 || pv.Layout[0].Container != "/workspace" {
		t.Fatalf("создание без папок: %d %s", code, body)
	}
	if pv.Mounts[0].Host != want {
		t.Fatalf("дефолтная папка: %s, ждём %s", pv.Mounts[0].Host, want)
	}
	if _, err := os.Stat(pv.Mounts[0].Host); err != nil {
		t.Fatalf("дефолтная папка не создана: %v", err)
	}
}

// Системный диалог папки: путь возвращается как есть, отмена и отсуствие
// утилиты различимы.
func TestLocalFSDialog(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	saved := dialogHook
	t.Cleanup(func() { dialogHook = saved })

	// Путь — настоящий абсолютный путь хост-ОС: обработчик прогоняет его
	// через filepath.Abs и EvalSymlinks, а POSIX-литерал на Windows
	// превращается в D:\… (CI windows-latest).
	picked := t.TempDir()
	want, err := filepath.EvalSymlinks(picked)
	if err != nil {
		t.Fatal(err)
	}
	dialogHook = func() (string, bool, error) { return picked, false, nil }
	code, body := env.do(c, "POST", "/api/local/ui/fs/dialog", nil)
	got := decode[struct {
		Cancelled bool   `json:"cancelled"`
		Path      string `json:"path"`
	}](t, body)
	if code != 200 || got.Cancelled || got.Path != want {
		t.Fatalf("диалог: %d %s", code, body)
	}

	dialogHook = func() (string, bool, error) { return "", true, nil }
	if code, body := env.do(c, "POST", "/api/local/ui/fs/dialog", nil); code != 200 || !strings.Contains(body, `"cancelled":true`) {
		t.Fatalf("отмена: %d %s", code, body)
	}

	dialogHook = func() (string, bool, error) { return "", false, os.ErrNotExist }
	if code, _ := env.do(c, "POST", "/api/local/ui/fs/dialog", nil); code != http.StatusConflict {
		t.Fatalf("сбой диалога: %d", code)
	}
}
