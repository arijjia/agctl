package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

// ── тестовое окружение ───────────────────────────────────────────────────────

// fakeRuntime — мок docker-рантайма: контейнеры «существуют», логи содержат
// boot-ссылку dsh с токеном (как печатает реальный контейнер).
type fakeRuntime struct {
	started    map[string]int // wsID → сколько раз Start
	startOpts  map[string]dockerrt.StartOpts
	resumed    map[string]int
	stopped    map[string]int
	removed    map[string]int
	logs       map[string]string
	logTails   map[string]string
	providers  map[string]provision.Provider
	chatStarts map[string]int
	chatStops  map[string]int
	chatSpecs  map[string]agctlrt.ChatSpec
	sshActive  map[string]bool
	chatHome   string
	failNext   error             // если задано — Start возвращает ошибку
	health     map[string]string // containerID → Docker health ("unhealthy"…)
	// chatEndpointOverride — endpoint для следующего handle от EnsureChat;
	// по умолчанию фейковый мёртвый порт 40000+userID.
	chatEndpointOverride string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		started: map[string]int{}, startOpts: map[string]dockerrt.StartOpts{}, resumed: map[string]int{},
		stopped: map[string]int{}, removed: map[string]int{}, logs: map[string]string{}, logTails: map[string]string{}, providers: map[string]provision.Provider{},
		chatStarts: map[string]int{}, chatStops: map[string]int{}, chatSpecs: map[string]agctlrt.ChatSpec{}, sshActive: map[string]bool{},
		health: map[string]string{},
	}
}

// HealthStatus — fake для healthReporter (Docker health из HEALTHCHECK).
func (f *fakeRuntime) HealthStatus(ctx context.Context, containerID string) (string, error) {
	return f.health[containerID], nil
}

func (f *fakeRuntime) Start(ctx context.Context, ws provision.Layout, wsID string, opts dockerrt.StartOpts, p provision.Provider) (dockerrt.StartResult, error) {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return dockerrt.StartResult{}, err
	}
	f.started[wsID]++
	f.startOpts[wsID] = opts
	f.providers[wsID] = p
	f.logs[wsID] = fmt.Sprintf("dsh web listening on http://0.0.0.0:3080/?token=fake-boot-token-%s", wsID)
	return dockerrt.StartResult{ContainerID: "ctr-" + wsID, HostPort: opts.HostPort}, nil
}

func (f *fakeRuntime) Resume(ctx context.Context, containerID string) error {
	f.resumed[containerID]++
	if f.stopped[containerID] > 0 {
		return fmt.Errorf("container not found")
	}
	return nil
}

func (f *fakeRuntime) Stop(ctx context.Context, containerID string) error {
	f.stopped[containerID]++
	return nil
}

func (f *fakeRuntime) Remove(ctx context.Context, containerID string) error {
	f.removed[containerID]++
	delete(f.logs, strings.TrimPrefix(containerID, "ctr-"))
	return nil
}

func (f *fakeRuntime) Logs(ctx context.Context, containerID, tailN string) (string, error) {
	f.logTails[containerID] = tailN
	return f.logs[strings.TrimPrefix(containerID, "ctr-")], nil
}

func (f *fakeRuntime) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	f.chatStarts[spec.RuntimeID]++
	f.chatSpecs[spec.RuntimeID] = spec
	base := filepath.Join(f.chatHome, "chats", spec.StorageID)
	dshHome, work := filepath.Join(base, "dsh-home"), filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if _, err := os.Stat(filepath.Join(dshHome, "profiles", "web", "package.json")); err != nil {
		if err := provision.SeedDshHome(dshHome, spec.Provider, assetsDshSkel); err != nil {
			return agctlrt.ChatHandle{}, err
		}
	}
	if err := provision.RefreshProvider(dshHome, spec.Provider); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	if err := provision.EnsureChatPlugins(dshHome, assetsDshSkel); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	instanceID := "chat-ctr-" + spec.RuntimeID
	f.logs[instanceID] = "dsh web listening on http://127.0.0.1/?token=fake-chat-" + spec.RuntimeID
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", 40000+spec.UserID)
	if f.chatEndpointOverride != "" {
		endpoint = f.chatEndpointOverride
	}
	sshPort := 0
	if spec.SSHPubKey != "" {
		sshPort = 2222 // как публикует реальный dockerrt.EnsureChat
	}
	return agctlrt.ChatHandle{
		RuntimeID: spec.RuntimeID, InstanceID: instanceID,
		Endpoint: endpoint, SSHPort: sshPort,
	}, nil
}

func (f *fakeRuntime) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	f.chatStops[handle.RuntimeID]++
	return nil
}

func (f *fakeRuntime) LogsChat(ctx context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	return f.logs[handle.InstanceID], nil
}

func (f *fakeRuntime) HasActiveSSH(ctx context.Context, handle agctlrt.ChatHandle) (bool, error) {
	return f.sshActive[handle.RuntimeID], nil
}

// testEnv — сервер на временных каталогах + httptest.
type testEnv struct {
	t     *testing.T
	srv   *Server
	api   *httptest.Server
	rt    *fakeRuntime
	home  string
	db    *store.DB
	admin *http.Client // клиент с cookiejar (первый пользователь — админ)
}

func newTestEnv(t *testing.T, maxPerUser int) *testEnv {
	return newTestEnvWith(t, maxPerUser, nil)
}

// newTestEnvWith позволяет тестам включать experimental-флаги конфигурации.
func newTestEnvWith(t *testing.T, maxPerUser int, tune func(*Config)) *testEnv {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	keyPath, err := secrets.EnsureMasterKey(home)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	rt := newFakeRuntime()
	rt.chatHome = home
	cfg := Config{Home: home, Addr: "test", MaxPerUser: maxPerUser, AutosaveEvery: time.Hour}
	if tune != nil {
		tune(&cfg)
	}
	s, err := NewWithRuntimeProviders(db, sec, cfg, rt, rt)
	if err != nil {
		t.Fatalf("server runtime injection: %v", err)
	}
	// минимальный веб-корень для статики SPA
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>agctl</title>")}}
	api := httptest.NewServer(s.Routes(webFS))
	t.Cleanup(api.Close)
	t.Cleanup(func() { _ = db.Close() })
	env := &testEnv{t: t, srv: s, api: api, rt: rt, home: home, db: db}
	env.admin = env.newClient()
	return env
}

func (e *testEnv) newClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Jar:           newTestJar(),
	}
}

// testJar — cookiejar, который не выкидывает cookie для 127.0.0.1.
type testJar struct{ m map[string][]*http.Cookie }

func newTestJar() *testJar { return &testJar{m: map[string][]*http.Cookie{}} }

func (j *testJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.m[u.Host] = append(j.m[u.Host], cookies...)
}
func (j *testJar) Cookies(u *url.URL) []*http.Cookie { return j.m[u.Host] }

// do — запрос с JSON-телом; возвращает статус и тело ответа.
func (e *testEnv) do(c *http.Client, method, path string, body any) (int, string) {
	e.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.api.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// register — POST /api/register; клиент запоминает сессионную cookie.
func (e *testEnv) register(c *http.Client, login, pass string) int {
	code, body := e.do(c, "POST", "/api/register", map[string]string{"login": login, "password": pass})
	if code != 200 {
		e.t.Fatalf("register %s: %d %s", login, code, body)
	}
	return code
}

// waitStatus — ждёт статус воркспейса (provision идёт в фоне).
func (e *testEnv) waitStatus(wsID, want string) store.Workspace {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ws, err := e.db.WorkspaceByID(wsID)
		if err == nil && ws.Status == want {
			return ws
		}
		time.Sleep(30 * time.Millisecond)
	}
	ws, _ := e.db.WorkspaceByID(wsID)
	e.t.Fatalf("воркспейс %s не пришёл к статусу %q (сейчас %+v)", wsID, want, ws.Status)
	return ws
}

func wsIDof(t *testing.T, body string) string {
	t.Helper()
	var out struct {
		Workspace struct {
			ID string `json:"id"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("не разобрать ответ: %s (%v)", body, err)
	}
	if out.Workspace.ID == "" {
		t.Fatalf("в ответе нет id воркспейса: %s", body)
	}
	return out.Workspace.ID
}

func validCreate(name string) map[string]any {
	return map[string]any{
		"name": name, "template": "base",
		"baseURL": "https://litellm.example/v1",
		"models":  []string{"qwen-test", "gpt-test"},
		"apiKey":  "sk-it-test-token",
	}
}

// ── аутентификация и роли ───────────────────────────────────────────────────

// TestRegisterFirstUserIsAdmin — первый зарегистрированный становится админом,
// остальные — обычными пользователями.
func TestRegisterFirstUserIsAdmin(t *testing.T) {
	e := newTestEnv(t, 3)
	code, body := e.do(e.admin, "POST", "/api/register", map[string]string{"login": "anna", "password": "secret1"})
	if code != 200 || !strings.Contains(body, `"admin"`) {
		t.Fatalf("первый пользователь должен быть админом: %d %s", code, body)
	}
	bob := e.newClient()
	code, body = e.do(bob, "POST", "/api/register", map[string]string{"login": "bob", "password": "secret1"})
	if code != 200 || !strings.Contains(body, `"user"`) {
		t.Fatalf("второй пользователь должен быть user: %d %s", code, body)
	}
}

// TestRegisterValidation — короткий логин/пароль и дубликат логина отклоняются.
func TestRegisterValidation(t *testing.T) {
	e := newTestEnv(t, 3)
	if code, _ := e.do(e.admin, "POST", "/api/register", map[string]string{"login": "ab", "password": "secret1"}); code != 400 {
		t.Fatalf("короткий логин: %d, ждали 400", code)
	}
	if code, _ := e.do(e.admin, "POST", "/api/register", map[string]string{"login": "anna", "password": "123"}); code != 400 {
		t.Fatalf("короткий пароль: %d, ждали 400", code)
	}
	if code, _ := e.do(e.admin, "POST", "/api/register", map[string]string{"login": "anna", "password": "secret1"}); code != 200 {
		t.Fatalf("валидная регистрация: %d, ждали 200", code)
	}
	if code, _ := e.do(e.newClient(), "POST", "/api/register", map[string]string{"login": "anna", "password": "secret1"}); code != 400 {
		t.Fatalf("дубликат логина: %d, ждали 400", code)
	}
	if code, _ := e.do(e.newClient(), "POST", "/api/register", nil); code != 400 {
		t.Fatalf("битый JSON (пустое тело): %d, ждали 400", code)
	}
}

// TestLoginLogoutMe — вход/выход/whoami; неверный пароль — 401.
func TestLoginLogoutMe(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")

	c := e.newClient()
	if code, _ := e.do(c, "POST", "/api/login", map[string]string{"login": "anna", "password": "wrong!!"}); code != 401 {
		t.Fatalf("неверный пароль: %d, ждали 401", code)
	}
	if code, body := e.do(c, "POST", "/api/login", map[string]string{"login": "anna", "password": "secret1"}); code != 200 || !strings.Contains(body, "anna") {
		t.Fatalf("login: %d %s", code, body)
	}
	if code, body := e.do(c, "GET", "/api/me", nil); code != 200 || !strings.Contains(body, `"admin"`) {
		t.Fatalf("me: %d %s", code, body)
	}
	if code, _ := e.do(c, "POST", "/api/logout", nil); code != 200 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := e.do(c, "GET", "/api/me", nil); code != 401 {
		t.Fatalf("me после logout: %d, ждали 401", code)
	}
}

// TestLoginRateLimit — после 10 неудачных попыток логин блокируется на 5 минут.
func TestLoginRateLimit(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	c := e.newClient()
	last := 0
	for i := 0; i < 11; i++ {
		last, _ = e.do(c, "POST", "/api/login", map[string]string{"login": "anna", "password": "nope-nope"})
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("после 11 неудач: %d, ждали 429", last)
	}
	// даже верный пароль теперь не пускает, пока счётчик не истёк
	if code, _ := e.do(c, "POST", "/api/login", map[string]string{"login": "anna", "password": "secret1"}); code != 429 {
		t.Fatalf("верный пароль под блокировкой: %d, ждали 429", code)
	}
}

// TestAuthRequired — без сессии API отдаёт 401, админ-эндпоинты — 403 для user.
func TestAuthRequired(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1") // админ
	bob := e.newClient()
	e.register(bob, "bob", "secret1") // обычный

	anon := e.newClient()
	if code, _ := e.do(anon, "GET", "/api/workspaces", nil); code != 401 {
		t.Fatalf("без сессии: %d, ждали 401", code)
	}
	if code, _ := e.do(anon, "GET", "/api/templates", nil); code != 401 {
		t.Fatalf("templates без сессии: %d, ждали 401", code)
	}
	if code, _ := e.do(bob, "GET", "/api/admin/users", nil); code != 403 {
		t.Fatalf("admin/users для user: %d, ждали 403", code)
	}
	if code, _ := e.do(bob, "GET", "/api/admin/audit", nil); code != 403 {
		t.Fatalf("admin/audit для user: %d, ждали 403", code)
	}
	if code, _ := e.do(e.admin, "GET", "/api/admin/users", nil); code != 200 {
		t.Fatalf("admin/users для админа: %d, ждали 200", code)
	}
}

// ── воркспейсы: полный жизненный цикл ───────────────────────────────────────

// TestKitsCatalogRemainsButLegacyProvisionIsRetired — каталог шаблонов
// сохраняется как источник для будущих v3-проектов, но отдельные места
// создавать больше нельзя.
func TestKitsCatalogRemainsButLegacyProvisionIsRetired(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	if code, _ := e.do(&http.Client{}, "GET", "/api/kits", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/kits: %d", code)
	}
	code, body := e.do(e.admin, "GET", "/api/kits", nil)
	if code != http.StatusOK || !strings.Contains(body, `"version":"1.0.0"`) || !strings.Contains(body, "1c-tooling") {
		t.Fatalf("kits: %d %s", code, body)
	}
	if code, body = e.do(e.admin, "POST", "/api/workspaces", validCreate("components")); code != http.StatusGone {
		t.Fatalf("legacy provision = %d %s, want 410", code, body)
	}
	if len(e.rt.started) != 0 {
		t.Fatal("legacy endpoint запустил отдельный runtime")
	}
}

// TestTemplatesEndpoint — legacy-каталог остаётся доступен старым клиентам.
func TestTemplatesEndpoint(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "GET", "/api/templates", nil)
	if code != 200 || !strings.Contains(body, "base") || !strings.Contains(body, "1c-vibe") {
		t.Fatalf("templates: %d %s", code, body)
	}
}

// TestWsTokenTakesLastMatch — регрессия баг-фикса: при docker start того же
// контейнера в журнале накапливаются несколько boot-строк; сервер обязан
// отдавать ПОСЛЕДНИЙ токен (первый к этому моменту устарел — dsh печатает
// новый при каждом старте, старый даёт 401).
func TestWsTokenTakesLastMatch(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	// Чистая запись для регрессии шлюза: новые legacy-места создавать нельзя,
	// но wsToken остаётся нужна для совместимости со старыми данными.
	wsID := "retok-fixture"
	ws := store.Workspace{ID: wsID, OwnerID: 1, Name: "retok", Status: "running",
		ContainerID: "ctr-" + wsID, HostPort: 43210, BaseURL: "https://llm.example/v1", Model: "m1"}
	if err := e.db.InsertWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	// эмулируем рестарт контейнера: в журнале две boot-строки
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=OLD-stale-token\n" +
		"dsh web: http://0.0.0.0:3080/?token=NEW-fresh-token"
	e.srv.forgetToken(wsID) // сброс кэша — как при start/пересоздании

	// wsToken обязан взять последний токен (шлюз подставит его при mint)
	ws, err := e.db.WorkspaceByID(wsID)
	if err != nil {
		t.Fatal(err)
	}
	tok := e.srv.wsToken(context.Background(), ws)
	if tok != "NEW-fresh-token" {
		t.Fatalf("wsToken взял устаревший токен: %q", tok)
	}
	// и кэш теперь хранит свежий токен
	e.srv.tokMu.Lock()
	cached := e.srv.tokens[wsID]
	e.srv.tokMu.Unlock()
	if cached != "NEW-fresh-token" {
		t.Fatalf("в кэше %q, ждали NEW-fresh-token", cached)
	}
	// ссылка воркспейса не содержит токена (он не покидает сервер)
	if u := workspaceURL(ws); strings.Contains(u, "token") || !strings.HasPrefix(u, "/w/") {
		t.Fatalf("workspaceURL %q — токен не должен попадать в URL", u)
	}
}
