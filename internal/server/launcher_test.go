package server

// launcher_test.go — лаунчер developer'а (карточка developer-launcher-images):
// каталог образов, выбор образа → ChatSpec.Image, старт/стоп рантайма,
// маршрутизация developer → /launcher.

import (
	"net/http"
	"testing"
)

// makeDeveloper — регистрирует пользователя и апрувит ему роль developer.
func makeDeveloper(env *testEnv, login, pass string) *http.Client {
	c := env.newClient()
	if code := env.register(c, login, pass); code != 200 {
		env.t.Fatalf("register %s: %d", login, code)
	}
	if code, _ := env.do(c, "POST", "/api/role-requests", map[string]string{"motivation": "лаунчер"}); code != 200 {
		env.t.Fatalf("role-request %s: %d", login, code)
	}
	if code, _ := env.do(env.admin, "POST", "/api/role-requests/1/approve", nil); code != 200 {
		env.t.Fatalf("approve %s: %d", login, code)
	}
	if code, body := env.do(c, "GET", "/api/me", nil); code != 200 || !contains(body, `"role":"developer"`) {
		env.t.Fatalf("me %s = %d %s, ждём role developer", login, code, body)
	}
	return c
}

func TestLauncherAccessAndCatalog(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.Tier0 = true
		c.DevImages = []string{"registry.university.example/agctl-dsh:0.2.2", "registry.university.example/agctl-dsh-nossh:0.2.2"}
	})
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "dev1", "devpass1")
	user := env.newClient()
	if code := env.register(user, "plain", "plainpw1"); code != 200 {
		t.Fatalf("register: %d", code)
	}

	// обычный пользователь не видит каталог
	if code, _ := env.do(user, "GET", "/api/launcher/images", nil); code != 403 {
		t.Fatalf("user launcher: %d, ждём 403", code)
	}
	// админ видит каталог (read-only), но выбор/старт/стоп ему закрыты
	// (см. TestLauncherStartStop)
	if code, _ := env.do(env.admin, "GET", "/api/launcher/images", nil); code != 200 {
		t.Fatalf("admin launcher: %d, ждём 200", code)
	}
	// developer видит каталог и дефолт
	code, body := env.do(dev, "GET", "/api/launcher/images", nil)
	if code != 200 || !contains(body, "agctl-dsh-nossh") {
		t.Fatalf("launcher images: %d %s", code, body)
	}

	// выбор образа из каталога
	if code, _ := env.do(dev, "PUT", "/api/me/image", map[string]string{
		"image": "registry.university.example/agctl-dsh-nossh:0.2.2",
	}); code != 200 {
		t.Fatalf("pick image: %d", code)
	}
	// образ вне каталога — 400
	if code, _ := env.do(dev, "PUT", "/api/me/image", map[string]string{"image": "evil:latest"}); code != 400 {
		t.Fatalf("pick вне каталога: %d, ждём 400", code)
	}
	// выбор пользователем запрещён
	if code, _ := env.do(user, "PUT", "/api/me/image", map[string]string{"image": "x"}); code != 403 {
		t.Fatalf("user pick: %d, ждём 403", code)
	}
	// текущий выбор вернулся в images
	_, body = env.do(dev, "GET", "/api/launcher/images", nil)
	if !contains(body, `"current":"registry.university.example/agctl-dsh-nossh:0.2.2"`) {
		t.Fatalf("current не вернулся: %s", body)
	}
}

func TestLauncherRootRouting(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "dev1", "devpass1")
	// developer: корень → /launcher (контейнер НЕ стартует)
	req, _ := http.NewRequest("GET", env.api.URL+"/", nil)
	resp, err := dev.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "/launcher" {
		t.Fatalf("developer GET /: %d → %s, ждём 307 → /launcher", resp.StatusCode, resp.Header.Get("Location"))
	}
	// лаунчер-страница отдаёт SPA
	req2, _ := http.NewRequest("GET", env.api.URL+"/launcher", nil)
	resp2, err := env.newClient().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Fatalf("GET /launcher: %d, ждём 200", resp2.StatusCode)
	}
}

func TestLauncherImageSwitchRecreatesRuntime(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.Tier0 = true
		c.DevImages = []string{"img-a:1", "img-b:1"}
	})
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "dev1", "devpass1")
	uid := int64(2)
	rid := env.srv.chatRuntimeID(uid)

	if code, _ := env.do(dev, "PUT", "/api/me/image", map[string]string{"image": "img-a:1"}); code != 200 {
		t.Fatalf("pick img-a: %d", code)
	}
	if code, _ := env.do(dev, "POST", "/api/me/runtime/start", nil); code != 200 {
		t.Fatalf("start img-a: %d", code)
	}
	if env.rt.chatStarts[rid] != 1 {
		t.Fatalf("starts = %d, ждём 1", env.rt.chatStarts[rid])
	}

	// смена образа на ЖИВОМ рантайме: старт пересоздаёт контейнер
	if code, _ := env.do(dev, "PUT", "/api/me/image", map[string]string{"image": "img-b:1"}); code != 200 {
		t.Fatalf("pick img-b: %d", code)
	}
	if code, _ := env.do(dev, "POST", "/api/me/runtime/start", nil); code != 200 {
		t.Fatalf("start img-b: %d", code)
	}
	if env.rt.chatStops[rid] != 1 {
		t.Fatalf("stops = %d, ждём 1 (старый контейнер уснут)", env.rt.chatStops[rid])
	}
	if env.rt.chatStarts[rid] != 2 {
		t.Fatalf("starts = %d, ждём 2 (пересоздание на новом образе)", env.rt.chatStarts[rid])
	}
	spec := env.rt.chatSpecs[rid]
	if spec.Image != "img-b:1" {
		t.Fatalf("spec.Image = %+v, ждём img-b:1", spec)
	}
	env.srv.chatMu.Lock()
	rt := env.srv.chats[uid]
	cached := ""
	if rt != nil {
		cached = rt.image
	}
	env.srv.chatMu.Unlock()
	if cached != "img-b:1" {
		t.Fatalf("кэш помнит образ %q, ждём img-b:1", cached)
	}
}

func TestLauncherImageFlowsToChatSpec(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.Tier0 = true
		c.DevImages = []string{"img-a:1", "img-b:1"}
	})
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "dev1", "devpass1")
	if code, _ := env.do(dev, "PUT", "/api/me/image", map[string]string{"image": "img-b:1"}); code != 200 {
		t.Fatalf("pick: %d", code)
	}
	// старт: ensureChat читает settings и кладёт Image в ChatSpec
	if code, _ := env.do(dev, "POST", "/api/me/runtime/start", nil); code != 200 {
		t.Fatalf("start: %d", code)
	}
	uid := int64(2) // первый — админ anna, dev1 второй
	spec, ok := env.rt.chatSpecs[env.srv.chatRuntimeID(uid)]
	if !ok {
		t.Fatalf("ChatSpec не зафиксирован; specs=%v", env.rt.chatSpecs)
	}
	if spec.Image != "img-b:1" {
		t.Fatalf("spec.Image = %q, ждём img-b:1", spec.Image)
	}
}

func TestLauncherStartStop(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	env.register(env.admin, "anna", "secret1")
	dev := makeDeveloper(env, "dev1", "devpass1")
	uid := int64(2)
	rid := env.srv.chatRuntimeID(uid)

	// старт
	code, body := env.do(dev, "POST", "/api/me/runtime/start", nil)
	if code != 200 || !contains(body, `"status":"running"`) {
		t.Fatalf("start: %d %s", code, body)
	}
	if env.rt.chatStarts[rid] != 1 {
		t.Fatalf("starts = %d, ждём 1", env.rt.chatStarts[rid])
	}
	// повторный старт — идемпотентен (то же окружение, без нового контейнера)
	if code, _ := env.do(dev, "POST", "/api/me/runtime/start", nil); code != 200 {
		t.Fatalf("start twice: %d", code)
	}
	if env.rt.chatStarts[rid] != 1 {
		t.Fatalf("starts после повтора = %d, ждём 1", env.rt.chatStarts[rid])
	}
	// стоп: compute удалён, кэш вытеснен, данные (фейковые тома) остались
	code, body = env.do(dev, "POST", "/api/me/runtime/stop", nil)
	if code != 200 || !contains(body, `"status":"sleeping"`) {
		t.Fatalf("stop: %d %s", code, body)
	}
	if env.rt.chatStops[rid] != 1 {
		t.Fatalf("stops = %d, ждём 1", env.rt.chatStops[rid])
	}
	env.srv.chatMu.Lock()
	evicted := env.srv.chats[uid] == nil
	env.srv.chatMu.Unlock()
	if !evicted {
		t.Fatal("runtime не вытеснен из кэша после stop")
	}
	// повторный стоп — идемпотентен
	if code, body = env.do(dev, "POST", "/api/me/runtime/stop", nil); code != 200 || !contains(body, `"status":"sleeping"`) {
		t.Fatalf("stop twice: %d %s", code, body)
	}
	// админ не может стартовать личный runtime
	if code, _ := env.do(env.admin, "POST", "/api/me/runtime/start", nil); code != 403 {
		t.Fatalf("admin start: %d, ждём 403", code)
	}
	if code, _ := env.do(env.admin, "POST", "/api/me/runtime/stop", nil); code != 403 {
		t.Fatalf("admin stop: %d, ждём 403", code)
	}
	if code, _ := env.do(env.admin, "PUT", "/api/me/image", map[string]string{"image": "img-a:1"}); code != 403 {
		t.Fatalf("admin pick: %d, ждём 403", code)
	}
}

// contains определён в map_test.go (strings.Contains); здесь переиспользуем.
