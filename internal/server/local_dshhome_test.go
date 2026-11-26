package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
)

// waitPlaceIdle — задача слота дошла до idle (старт места в фоне).
func waitPlaceIdle(t *testing.T, env *testEnv, slot int) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := env.srv.placeTask(slot).snapshot()
		if snap["state"] == "idle" {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("слот %d не дошёл до idle: %v", slot, env.srv.placeTask(slot).snapshot())
	return nil
}

// Явная папка dsh-home: место живёт в папке владельца, она становится
// git-репозиторием (.gitignore без секретов), первый коммит — после первого
// старта; папки внутри данных agctl и $HOME целиком не принимаются.
func TestLocalPlaceCustomDshHome(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("нет git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	dirs := mkdirs(t, t.TempDir(), "proj")
	dshHome := filepath.Join(t.TempDir(), "ag_dsh")

	bad := filepath.Join(env.srv.cfg.Home, "chats", "x")
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "Плохое", "id": "bad", "mounts": []map[string]string{{"host": dirs[0]}}, "dshHome": bad,
	}); code != http.StatusBadRequest || !strings.Contains(body, "данных agctl") {
		t.Fatalf("dsh-home внутри данных agctl: %d %s", code, body)
	}

	code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "Конфиг", "id": "conf", "mounts": []map[string]string{{"host": dirs[0]}}, "dshHome": dshHome,
	})
	if code != http.StatusCreated {
		t.Fatalf("место с dsh-home: %d %s", code, body)
	}
	view := decode[placeView](t, body)
	if !view.DshHomeCustom || view.DshHome != canonical(t, dshHome) || view.DshHomeGit == nil || !view.DshHomeGit.Repo || view.DshHomeGit.Commits {
		t.Fatalf("вью места: %+v git=%+v", view, view.DshHomeGit)
	}
	if _, err := os.Stat(filepath.Join(dshHome, ".gitignore")); err != nil {
		t.Fatalf(".gitignore: %v", err)
	}
	if code, body := env.do(c, "POST", "/api/local/ui/places/conf/start", nil); code != http.StatusAccepted {
		t.Fatalf("старт: %d %s", code, body)
	}
	snap := waitPlaceIdle(t, env, view.Slot)
	if !strings.Contains(strings.Join(snap["log"].([]string), "\n"), "первый коммит конфигурации") {
		t.Fatalf("лог старта: %v", snap["log"])
	}
	spec := env.rt.chatSpecs[env.srv.chatRuntimeID(env.srv.local.ownerID, view.Slot)]
	if spec.DshHome != canonical(t, dshHome) {
		t.Fatalf("рантайм получил dsh-home %q", spec.DshHome)
	}
	st := provision.DshHomeRepoState(dshHome)
	if !st.Commits || st.Dirty {
		t.Fatalf("репозиторий после старта: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(dshHome, "profiles", "web", "package.json")); err != nil {
		t.Fatalf("dsh-home не засеян в явной папке: %v", err)
	}

	// вернуть место в дом agctl: "" — папка владельца остаётся на диске
	empty := ""
	if code, body := env.do(c, "PUT", "/api/local/ui/places/conf", map[string]any{
		"name": "Конфиг", "revision": view.Revision, "mounts": []map[string]string{{"host": dirs[0]}}, "dshHome": empty,
	}); code != http.StatusOK || !strings.Contains(body, `"dshHomeCustom":false`) {
		t.Fatalf("сброс dsh-home: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dshHome, ".git")); err != nil {
		t.Fatalf("папка владельца не должна пропасть: %v", err)
	}
}

// Клон места: новое место с теми же папками и образом, копия dsh-home со
// скиллами и сессиями; запущенный источник на время копии останавливается и
// запускается снова; пресеты источника есть и у клона.
func TestLocalPlaceClone(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	owner := env.srv.local.ownerID
	if code, body := env.do(c, "POST", "/api/local/ui/presets", map[string]any{
		"name": "poisk", "mcp": []string{"ddg"}, "places": []string{"main"},
	}); code != http.StatusCreated {
		t.Fatalf("пресет: %d %s", code, body)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatal("старт основного")
	}
	env.waitTask("idle")
	mainHome := env.srv.dshHomeFor(userKey(owner))
	if err := os.MkdirAll(filepath.Join(mainHome, "sessions", "--workspace--", "s1"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mainHome, "sessions", "--workspace--", "s1", "session.v4.jsonl.zstd"), []byte("история"), 0o600); err != nil {
		t.Fatal(err)
	}
	startsBefore := env.rt.chatStarts[env.srv.chatRuntimeID(owner, 0)]

	code, body := env.do(c, "POST", "/api/local/ui/places/main/clone", map[string]any{"name": "Пробы"})
	if code != http.StatusCreated {
		t.Fatalf("клон: %d %s", code, body)
	}
	clone := decode[placeView](t, body)
	if clone.ID != "main-clone" || clone.Slot == 0 || len(clone.Mounts) != 1 || clone.Mounts[0].Host != env.srv.localWorkDir() {
		t.Fatalf("клон: %+v", clone)
	}
	raw, err := os.ReadFile(filepath.Join(clone.DshHome, "sessions", "--workspace--", "s1", "session.v4.jsonl.zstd"))
	if err != nil || string(raw) != "история" {
		t.Fatalf("сессия в клоне: %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(clone.DshHome, ".agent-presets", "agctl-poisk")); err != nil {
		t.Fatalf("пресет в клоне: %v", err)
	}
	if code, body := env.do(c, "GET", "/api/local/ui/presets", nil); code != 200 || !strings.Contains(body, `"main-clone"`) {
		t.Fatalf("пресет не привязан к клону: %d %s", code, body)
	}
	// источник был запущен — после копии он снова стартует
	env.waitTask("idle")
	deadline := time.Now().Add(5 * time.Second)
	for env.rt.chatStarts[env.srv.chatRuntimeID(owner, 0)] == startsBefore && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if env.rt.chatStarts[env.srv.chatRuntimeID(owner, 0)] == startsBefore {
		t.Fatal("исходное место после клона не запущено снова")
	}
	// второй клон — свободный id
	if code, body := env.do(c, "POST", "/api/local/ui/places/main/clone", map[string]any{}); code != http.StatusCreated || !strings.Contains(body, `"id":"main-clone-2"`) {
		t.Fatalf("второй клон: %d %s", code, body)
	}
	env.waitTask("idle") // источник снова стартует в фоне — дождаться до очистки TempDir
}

func canonical(t *testing.T, p string) string {
	t.Helper()
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// Пересечения явной папки dsh-home: занятая другим местом, папка проектов
// (своя или чужая) внутри неё, непустая папка с кодом — отказ; репозиторий
// dsh-home после git clone — можно. Папка основного места — в config.yaml
// (хуки), ранняя запись в settings переносится туда.
func TestLocalPlaceDshHomeChecks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("нет git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	env, h := newLocalUIEnv(t)
	c := loginOwner(t, env)
	owner := env.srv.local.ownerID
	base := t.TempDir()
	dirs := mkdirs(t, base, "a", "b")
	dshA := filepath.Join(t.TempDir(), "dsh-a")
	create := func(id, mount, dshHome string) (int, string) {
		return env.do(c, "POST", "/api/local/ui/places", map[string]any{
			"name": id, "id": id, "mounts": []map[string]string{{"host": mount}}, "dshHome": dshHome,
		})
	}
	if code, body := create("pa", dirs[0], dshA); code != http.StatusCreated {
		t.Fatalf("место A: %d %s", code, body)
	}
	for _, tc := range []struct{ name, dsh, want string }{
		{"папка другого места", dshA, "уже занята"},
		{"внутри папки другого места", filepath.Join(dshA, "sub"), "уже занята"},
		{"папка проектов другого места", dirs[0], "папка проектов"},
		{"своя папка проектов внутри", base, "папка проектов"},
	} {
		if code, body := create("pb", dirs[1], tc.dsh); code != http.StatusBadRequest || !strings.Contains(body, tc.want) {
			t.Fatalf("%s: %d %s", tc.name, code, body)
		}
	}
	code := filepath.Join(t.TempDir(), "code")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(code, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, body := create("pb", dirs[1], code); status != http.StatusBadRequest || !strings.Contains(body, "не пуста") {
		t.Fatalf("папка с кодом: %d %s", status, body)
	}
	cloned := filepath.Join(t.TempDir(), "cloned")
	if err := os.MkdirAll(cloned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cloned, ".gitignore"), []byte(provision.DshHomeGitignore), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, body := create("pb", dirs[1], cloned); status != http.StatusCreated {
		t.Fatalf("репозиторий dsh-home после clone: %d %s", status, body)
	}

	// основное место: папка — в config.yaml через хуки, settings не пишется
	mainDsh := filepath.Join(t.TempDir(), "main-dsh")
	if status, body := env.do(c, "PUT", "/api/local/ui/places/main", map[string]any{
		"name": "Основное", "revision": env.srv.local.mainRevision(),
		"mounts": []map[string]string{{"host": env.srv.localWorkDir()}}, "dshHome": mainDsh,
	}); status != http.StatusOK || !strings.Contains(body, `"dshHomeCustom":true`) {
		t.Fatalf("dsh-home основного места: %d %s", status, body)
	}
	if got := h.MainDshHome(); got != filepath.Join(canonical(t, filepath.Dir(mainDsh)), "main-dsh") {
		t.Fatalf("config.yaml dsh_home = %q", got)
	}
	if v, _ := env.srv.db.SettingGet(dshHomeSettingKey(owner, 0)); v != "" {
		t.Fatalf("settings не должен хранить папку основного места: %q", v)
	}
	// ранняя запись в settings переезжает в config.yaml
	_ = h.SetMainDshHome("")
	early := filepath.Join(t.TempDir(), "early")
	if err := env.srv.db.SettingSet(dshHomeSettingKey(owner, 0), early); err != nil {
		t.Fatal(err)
	}
	if got := env.srv.customDshHome(userKey(owner)); got != early || h.MainDshHome() != early {
		t.Fatalf("перенос: custom=%q hooks=%q", got, h.MainDshHome())
	}
	if v, _ := env.srv.db.SettingGet(dshHomeSettingKey(owner, 0)); v != "" {
		t.Fatalf("после переноса settings пуст: %q", v)
	}
}
