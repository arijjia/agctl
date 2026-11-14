package server

// slots_test.go — мульти-рантайм developer'а (карточка
// developer-multi-containers): именование слотов и томов (совместимость
// слота 0), лимит слотов, жизненный цикл через API (image/start/stop/
// activate), видимость активного слота в /chat/ и /api/me/runtime,
// запрет слотов роли user.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSlotNamingAndStorageCompat(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "slotdev", "slotpass1")
	uid := devUserID(env, dev)

	// Слот 0 — исторические имена (без суффикса): существующие тома и
	// контейнеры не мигрируют.
	if got := env.srv.chatRuntimeID(uid, 0); strings.HasSuffix(got, "-s0") {
		t.Fatalf("слот 0 получил суффикс: %s", got)
	}
	if got := chatStorageID(uid, 0); got != fmt.Sprintf("u%d", uid) {
		t.Fatalf("слот 0 storage: %s", got)
	}
	// Слот 2 — суффикс в RuntimeID и плоский том.
	rid := env.srv.chatRuntimeID(uid, 2)
	if !strings.HasSuffix(rid, fmt.Sprintf("-u%d-s2", uid)) {
		t.Fatalf("RuntimeID слота 2: %s", rid)
	}
	if got := chatStorageID(uid, 2); got != fmt.Sprintf("u%ds2", uid) {
		t.Fatalf("StorageID слота 2: %s", got)
	}
}

// devUserID — ID пользователя по логину из /api/admin/users.
func devUserID(env *testEnv, c *http.Client) int64 {
	_, body := env.do(env.admin, "GET", "/api/admin/users", nil)
	var resp struct {
		Users []struct {
			ID    int64  `json:"id"`
			Login string `json:"login"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		env.t.Fatalf("admin users parse: %v: %s", err, body)
	}
	for _, u := range resp.Users {
		if u.Login == "slotdev" || u.Login == "multidev" {
			return u.ID
		}
	}
	env.t.Fatalf("developer не найден в %s", body)
	return 0
}

func TestSlotLifecycleAPI(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.Tier0 = true
		c.DevImages = []string{"img-a:1", "img-b:2"}
	})
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "multidev", "multipass1")

	// Образ и старт слота 2.
	if code, body := env.do(dev, "PUT", "/api/me/slots/2/image", map[string]string{"image": "img-b:2"}); code != 200 || !contains(body, `"image":"img-b:2"`) {
		t.Fatalf("slot image: %d %s", code, body)
	}
	if code, body := env.do(dev, "POST", "/api/me/slots/2/start", nil); code != 200 || !contains(body, `"status":"running"`) {
		t.Fatalf("slot start: %d %s", code, body)
	}
	uid := devUserID(env, dev)
	rid := env.srv.chatRuntimeID(uid, 2)
	spec, ok := env.rt.chatSpecs[rid]
	if !ok {
		t.Fatalf("слот 2 не поднялся: %v", env.rt.chatSpecs)
	}
	if spec.Image != "img-b:2" {
		t.Fatalf("образ слота 2: %q", spec.Image)
	}
	if spec.StorageID != fmt.Sprintf("u%ds2", uid) {
		t.Fatalf("StorageID слота 2: %q", spec.StorageID)
	}

	// Список слотов: слот 2 running, слот 0 absent.
	_, body := env.do(dev, "GET", "/api/me/slots", nil)
	if !contains(body, `"slot":2`) || !contains(body, `"state":"running"`) || !contains(body, `"state":"absent"`) {
		t.Fatalf("slots list: %s", body)
	}

	// Активация слота 2 → /api/me/runtime видит его рантайм.
	if code, _ := env.do(dev, "POST", "/api/me/slots/2/activate", nil); code != 200 {
		t.Fatalf("activate: %d", code)
	}
	if code, body := env.do(dev, "GET", "/api/me/runtime", nil); code != 200 || !contains(body, `"status":"running"`) {
		t.Fatalf("runtime after activate: %d %s", code, body)
	}

	// Стоп слота 2 → compute удалён, том остался.
	if code, body := env.do(dev, "POST", "/api/me/slots/2/stop", nil); code != 200 || !contains(body, `"status":"sleeping"`) {
		t.Fatalf("slot stop: %d %s", code, body)
	}
	if env.rt.chatStops[rid] != 1 {
		t.Fatalf("слот 2 не усыплён: %v", env.rt.chatStops)
	}
}

func TestSlotLimitAndRoleGuards(t *testing.T) {
	env := newTestEnvWith(t, 2, func(c *Config) { c.Tier0 = true; c.LauncherSlotsMax = 2 })
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "slotdev", "limitedpass1")
	user := env.newClient()
	env.register(user, "plain2", "plainpw2")

	// Слот за лимитом установки — 403.
	if code, _ := env.do(dev, "POST", "/api/me/slots/2/start", nil); code != 403 {
		t.Fatalf("slot за лимитом: %d, ждём 403", code)
	}
	if code, _ := env.do(dev, "PUT", "/api/me/slots/2/image", map[string]string{"image": "img-a:1"}); code != 403 {
		t.Fatalf("image за лимитом: %d, ждём 403", code)
	}
	// Роль user — лаунчер недоступен целиком (requireDeveloper): user имеет
	// один runtime (слот 0) и стартует его лениво через /chat/.
	if code, _ := env.do(user, "POST", "/api/me/slots/1/start", nil); code != 403 {
		t.Fatalf("user slot 1: %d, ждём 403", code)
	}
	if code, _ := env.do(user, "GET", "/api/me/slots", nil); code != 403 {
		t.Fatalf("user slots list: %d, ждём 403", code)
	}
	// Админ — отказ.
	if code, _ := env.do(env.admin, "GET", "/api/me/slots", nil); code != 403 {
		t.Fatalf("admin slots: %d, ждём 403", code)
	}
	// Мусор в пути слота — 403 (не 500).
	if code, _ := env.do(dev, "POST", "/api/me/slots/abc/start", nil); code == 200 {
		t.Fatalf("мусорный slot принят: %d", code)
	}
}
