package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loginOwner — клиент с сессией владельца локального режима.
func loginOwner(t *testing.T, env *testEnv) *http.Client {
	t.Helper()
	c := env.newClient()
	if code, _ := env.do(c, "GET", "/local/login?token=login-tok", nil); code != http.StatusSeeOther {
		t.Fatalf("вход владельца: %d", code)
	}
	return c
}

func mkdirs(t *testing.T, root string, names ...string) []string {
	t.Helper()
	var out []string
	for _, n := range names {
		p := filepath.Join(root, n)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		out = append(out, p)
	}
	return out
}

func decode[T any](t *testing.T, body string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatalf("json: %v: %s", err, body)
	}
	return v
}

// Рабочие места: основное всегда первым; новое создаётся, меняется только
// с актуальной ревизией (устаревшая — 409), удаляется с сохранением dsh-home.
func TestLocalPlacesCRUDAndRevisions(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	dirs := mkdirs(t, t.TempDir(), "alpha", "beta")

	code, body := env.do(c, "GET", "/api/local/ui/places", nil)
	list := decode[struct{ Places []placeView }](t, body)
	if code != 200 || len(list.Places) != 1 || !list.Places[0].Main || list.Places[0].URL != "/chat/" {
		t.Fatalf("список до создания: %d %s", code, body)
	}

	code, body = env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "Фронтенд", "id": "front", "mounts": []map[string]string{{"host": dirs[0]}},
	})
	pv := decode[placeView](t, body)
	if code != http.StatusCreated || pv.ID != "front" || pv.Revision != 1 || pv.URL != "/p/front/" ||
		len(pv.Layout) != 1 || pv.Layout[0].Container != "/workspace" || pv.State != "sleeping" {
		t.Fatalf("создание: %d %s", code, body)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "дубль", "id": "front", "mounts": []map[string]string{{"host": dirs[0]}},
	}); code != http.StatusConflict {
		t.Fatalf("дубль id: %d", code)
	}

	// две папки → каждая в /workspace/<имя>
	code, body = env.do(c, "PUT", "/api/local/ui/places/front", map[string]any{
		"name": "Фронтенд", "revision": 1, "mounts": []map[string]string{{"host": dirs[0]}, {"host": dirs[1]}},
	})
	pv = decode[placeView](t, body)
	if code != 200 || pv.Revision != 2 || len(pv.Layout) != 2 || pv.Layout[1].Container != "/workspace/beta" {
		t.Fatalf("изменение: %d %s", code, body)
	}
	// устаревшая ревизия — 409, запись не меняется
	if code, _ := env.do(c, "PUT", "/api/local/ui/places/front", map[string]any{
		"name": "Другое", "revision": 1, "mounts": []map[string]string{{"host": dirs[0]}},
	}); code != http.StatusConflict {
		t.Fatalf("устаревшая ревизия: %d", code)
	}

	code, body = env.do(c, "DELETE", "/api/local/ui/places/front", nil)
	if code != 200 || !strings.Contains(body, `"archived"`) {
		t.Fatalf("удаление: %d %s", code, body)
	}
	if code, _ := env.do(c, "DELETE", "/api/local/ui/places/main", nil); code != http.StatusBadRequest {
		t.Fatalf("удаление основного: %d", code)
	}
}

// Папки рабочего места проверяются как папка проектов agctl dev: корень,
// домашний каталог целиком и несуществующий путь — отказ.
func TestLocalPlacesRejectBadFolders(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	home, _ := os.UserHomeDir()
	for _, bad := range []string{"/", home, filepath.Join(t.TempDir(), "нет"), "relative/dir"} {
		if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{
			"name": "x", "id": "x", "mounts": []map[string]string{{"host": bad}},
		}); code != http.StatusBadRequest {
			t.Fatalf("папка %q: %d %s", bad, code, body)
		}
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "x", "id": "main", "mounts": []map[string]string{{"host": t.TempDir()}},
	}); code != http.StatusBadRequest {
		t.Fatalf("id main: %d", code)
	}
}

// Старт дополнительного места: свой RuntimeID и хранилище, одна папка —
// WorkDir, несколько — Mounts; основное место не затронуто.
func TestLocalPlaceRuntimeSpec(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	if code, _ := env.do(c, "POST", "/api/local/ui/places/main/start", nil); code != http.StatusAccepted {
		t.Fatalf("старт основного места: %d", code)
	}
	env.waitTask("idle")
	dirs := mkdirs(t, t.TempDir(), "one", "two")
	owner := env.srv.local.ownerID
	for _, p := range []struct {
		id     string
		mounts []map[string]string
	}{
		{"solo", []map[string]string{{"host": dirs[0]}}},
		{"duo", []map[string]string{{"host": dirs[0]}, {"host": dirs[1], "name": "lib"}}},
	} {
		if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": p.id, "id": p.id, "mounts": p.mounts}); code != 201 {
			t.Fatalf("создание %s: %d %s", p.id, code, body)
		}
		if code, _ := env.do(c, "POST", "/api/local/ui/places/"+p.id+"/start", nil); code != http.StatusAccepted {
			t.Fatalf("старт %s: %d", p.id, code)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		env.srv.chatMu.Lock()
		n := len(env.srv.chats)
		env.srv.chatMu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// рабочее место = слот: solo — слот 1, duo — слот 2 (младший свободный)
	solo, ok := env.rt.chatSpecs[env.srv.chatRuntimeID(owner, 1)]
	if !ok || solo.WorkDir != dirs[0] || len(solo.Mounts) != 0 || solo.StorageID != chatStorageID(owner, 1) || !solo.Local {
		t.Fatalf("spec solo: %+v", solo)
	}
	duo := env.rt.chatSpecs[env.srv.chatRuntimeID(owner, 2)]
	if duo.WorkDir != "" || len(duo.Mounts) != 2 || duo.Mounts[1].Name != "lib" || duo.Mounts[0].Name != "one" {
		t.Fatalf("spec duo: %+v", duo)
	}
	main := env.rt.chatSpecs[env.srv.chatRuntimeID(owner, 0)]
	if main.StorageID != chatStorageID(owner, 0) || main.WorkDir == "" {
		t.Fatalf("основное место изменилось: %+v", main)
	}
	// у дополнительного места свой dsh-home (файлы доступов агента туда)
	if _, err := os.Stat(filepath.Join(env.home, "chats", chatStorageID(owner, 2), "dsh-home")); err != nil {
		t.Fatalf("dsh-home места duo: %v", err)
	}
	// неизвестное место — 404 без попытки старта
	if code, _ := env.do(c, "GET", "/p/nope/", nil); code != http.StatusNotFound {
		t.Fatalf("неизвестное место: %d", code)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/places/nope/start", nil); code != http.StatusNotFound {
		t.Fatalf("старт неизвестного места: %d", code)
	}
}

// Дерево папок не выходит за домашний каталог; «Открыть» работает только
// внутри папок рабочих мест и их dsh-home.
func TestLocalFSAndRevealBounds(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	dirs := mkdirs(t, home, "src/app", "src/.hidden", "notes")
	if err := os.MkdirAll(filepath.Join(dirs[0], ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, body := env.do(c, "GET", "/api/local/ui/fs?path="+filepath.Join(home, "src"), nil)
	fsv := decode[struct {
		Entries    []fsEntry
		Parent     string
		Selectable bool
	}](t, body)
	if code != 200 || len(fsv.Entries) != 1 || fsv.Entries[0].Name != "app" || !fsv.Entries[0].Git || fsv.Parent != home || !fsv.Selectable {
		t.Fatalf("fs src: %d %s", code, body)
	}
	if code, _ := env.do(c, "GET", "/api/local/ui/fs?path=/etc", nil); code != http.StatusForbidden {
		t.Fatalf("fs вне $HOME: %d", code)
	}
	if code, body := env.do(c, "GET", "/api/local/ui/fs", nil); code != 200 || !strings.Contains(body, `"selectable":false`) {
		t.Fatalf("сам $HOME не выбирается: %d %s", code, body)
	}

	if code, _ := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "app", "id": "app", "mounts": []map[string]string{{"host": dirs[0]}},
	}); code != 201 {
		t.Fatalf("создание: %d", code)
	}
	var opened []string
	revealHook = func(app, p string) (*exec.Cmd, error) {
		opened = append(opened, app+":"+p)
		return exec.Command("true"), nil
	}
	t.Cleanup(func() { revealHook = nil })
	if code, body := env.do(c, "POST", "/api/local/ui/reveal", map[string]string{"path": dirs[0], "app": "vscode"}); code != 200 {
		t.Fatalf("reveal в папке места: %d %s", code, body)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/reveal", map[string]string{"path": dirs[2], "app": "finder"}); code != http.StatusForbidden {
		t.Fatalf("reveal вне мест: %d", code)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/reveal", map[string]string{"path": dirs[0] + "/../../notes", "app": "finder"}); code != http.StatusForbidden {
		t.Fatalf("reveal с ../: %d", code)
	}
	if len(opened) != 1 || opened[0] != "vscode:"+dirs[0] {
		t.Fatalf("открыто: %v", opened)
	}
}

// Поток событий: создание места приходит событием «places».
func TestLocalEventsStream(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	req, _ := http.NewRequest("GET", env.api.URL+"/api/local/ui/events", nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type: %q", ct)
	}
	got := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				got <- sc.Text()
			}
		}
	}()
	time.Sleep(50 * time.Millisecond) // подписка зарегистрирована
	if code, _ := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "ev", "id": "ev", "mounts": []map[string]string{{"host": t.TempDir()}},
	}); code != 201 {
		t.Fatalf("создание: %d", code)
	}
	select {
	case line := <-got:
		if !strings.Contains(line, `"topic":"places"`) {
			t.Fatalf("событие: %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("событие places не пришло")
	}
}

// Серверный режим: маршрутов рабочих мест лаунчера v2 нет (слоты там —
// API /api/me/slots модуля launcher).
func TestServerModeHasNoPlaces(t *testing.T) {
	env := newTestEnv(t, 1)
	c := env.newClient()
	env.do(c, "POST", "/api/register", map[string]string{"login": "u1", "password": "Password-123!"})
	for _, p := range []string{"/api/local/ui/places", "/p/x/"} {
		if code, _ := env.do(c, "GET", p, nil); code != http.StatusNotFound {
			t.Fatalf("%s в серверном режиме: %d", p, code)
		}
	}
}

// Страницы лаунчера в локальном режиме отдаются SPA (модуль launcher здесь
// не подключён): полный лаунчер и встроенный режим для табов dsh.
func TestLocalLauncherPagesServed(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	for _, p := range []string{"/launcher", "/launcher/embed/models", "/launcher/embed/places"} {
		if code, body := env.do(c, "GET", p, nil); code != 200 || !strings.Contains(body, "<title>") {
			t.Fatalf("%s: %d %.80s", p, code, body)
		}
	}
}

// Удаление места освобождает слот: данные слота уходят в архив, новое место
// на том же слоте начинает с чистого dsh-home и без чужого образа.
func TestLocalPlaceDeleteFreesSlot(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	owner := env.srv.local.ownerID
	dirs := mkdirs(t, t.TempDir(), "a", "b")
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": "a", "id": "a", "image": "img:a", "mounts": []map[string]string{{"host": dirs[0]}}}); code != 201 {
		t.Fatalf("создание a: %d %s", code, body)
	}
	if got := env.srv.developerChatImage(owner, 1); got != "img:a" {
		t.Fatalf("образ слота 1 = %q", got)
	}
	slotDir := filepath.Join(env.home, "chats", chatStorageID(owner, 1))
	if err := os.MkdirAll(filepath.Join(slotDir, "dsh-home"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, body := env.do(c, "DELETE", "/api/local/ui/places/a", nil)
	if code != 200 || !strings.Contains(body, ".deleted-") {
		t.Fatalf("удаление: %d %s", code, body)
	}
	if _, err := os.Stat(slotDir); !os.IsNotExist(err) {
		t.Fatalf("данные слота остались на месте: %v", err)
	}
	if got := env.srv.developerChatImage(owner, 1); got != "" {
		t.Fatalf("образ удалённого места остался: %q", got)
	}
	code, body = env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": "b", "id": "b", "mounts": []map[string]string{{"host": dirs[1]}}})
	if code != 201 || !strings.Contains(body, `"slot":1`) {
		t.Fatalf("новое место не на освободившемся слоте 1: %d %s", code, body)
	}
}

// Мест не больше, чем слотов: слот 0 — основное, остальные — по одному.
func TestLocalPlaceSlotLimit(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	env.srv.cfg.LauncherSlotsMax = 2
	c := loginOwner(t, env)
	dirs := mkdirs(t, t.TempDir(), "a", "b")
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": "a", "id": "a", "mounts": []map[string]string{{"host": dirs[0]}}}); code != 201 {
		t.Fatalf("первое место: %d %s", code, body)
	}
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{"name": "b", "id": "b", "mounts": []map[string]string{{"host": dirs[1]}}}); code != http.StatusConflict {
		t.Fatalf("сверх лимита слотов: %d %s", code, body)
	}
}
