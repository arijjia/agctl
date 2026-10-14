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

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

// ── тестовое окружение ───────────────────────────────────────────────────────

// fakeRuntime — мок docker-рантайма: контейнеры «существуют», логи содержат
// boot-ссылку dsh с токеном (как печатает реальный контейнер).
type fakeRuntime struct {
	started  map[string]int // wsID → сколько раз Start
	resumed  map[string]int
	stopped  map[string]int
	removed  map[string]int
	logs     map[string]string
	failNext error // если задано — Start возвращает ошибку
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		started: map[string]int{}, resumed: map[string]int{},
		stopped: map[string]int{}, removed: map[string]int{}, logs: map[string]string{},
	}
}

func (f *fakeRuntime) Start(ctx context.Context, ws provision.Layout, wsID string, hostPort int, p provision.Provider) (dockerrt.StartResult, error) {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return dockerrt.StartResult{}, err
	}
	f.started[wsID]++
	f.logs[wsID] = fmt.Sprintf("dsh web listening on http://0.0.0.0:3080/?token=fake-boot-token-%s", wsID)
	return dockerrt.StartResult{ContainerID: "ctr-" + wsID, HostPort: hostPort}, nil
}

func (f *fakeRuntime) Resume(ctx context.Context, containerID string) error {
	f.resumed[containerID]++
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
	return f.logs[strings.TrimPrefix(containerID, "ctr-")], nil
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
	s := &Server{
		db: db, sec: sec, rt: rt,
		cfg:    Config{Home: home, Addr: "test", MaxPerUser: maxPerUser, AutosaveEvery: time.Hour},
		tokens: map[string]string{},
	}
	s.authN = &auth.Service{
		SetSession: db.CreateSession,
		SessionUser: func(token string) (int64, string, string, error) {
			u, err := db.SessionUser(token)
			if err != nil {
				return 0, "", "", err
			}
			return u.ID, u.Login, u.Role, nil
		},
		DeleteSession: db.DeleteSession,
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

// TestWorkspaceLifecycle — создание → running → stop → start → delete,
// с проверкой файловой раскладки и аудита.
func TestWorkspaceLifecycle(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")

	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("demo ws"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "running")
	if ws.ContainerID == "" || ws.HostPort == 0 {
		t.Fatalf("runtime не записан: %+v", ws)
	}
	if e.rt.started[wsID] != 1 {
		t.Fatalf("Start вызван %d раз, ждали 1", e.rt.started[wsID])
	}

	// файловая часть: bare + клон + dsh-home
	paths := provision.Paths(e.home, "anna", ws.ID, ws.Name)
	for _, f := range []string{
		filepath.Join(paths.Repo, "HEAD"),
		filepath.Join(paths.Work, "AGENTS.md"),
		filepath.Join(paths.DshHome, "settings.yaml"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("нет %s: %v", f, err)
		}
	}

	// токен не утекает в API
	code, body = e.do(e.admin, "GET", "/api/workspaces/"+wsID, nil)
	if code != 200 {
		t.Fatalf("get: %d", code)
	}
	if strings.Contains(body, "sk-it-test-token") {
		t.Fatal("API-токен LLM утёк в ответе GET воркспейса")
	}

	// logs
	code, body = e.do(e.admin, "GET", "/api/workspaces/"+wsID+"/logs", nil)
	if code != 200 || !strings.Contains(body, "fake-boot-token") {
		t.Fatalf("logs: %d %s", code, body)
	}

	// stop → stopped
	if code, _ := e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/stop", nil); code != http.StatusAccepted {
		t.Fatalf("stop: %d", code)
	}
	e.waitStatus(wsID, "stopped")
	if e.rt.stopped["ctr-"+wsID] != 1 {
		t.Fatal("runtime.Stop не вызван")
	}

	// повторный stop уже остановленного — конфликт/идемпотентность
	code, _ = e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/stop", nil)
	if code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("повторный stop: %d", code)
	}

	// start → running снова (контейнер есть — Resume)
	if code, _ := e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/start", nil); code != http.StatusAccepted {
		t.Fatalf("start: %d", code)
	}
	e.waitStatus(wsID, "running")
	if e.rt.resumed["ctr-"+wsID] == 0 && e.rt.started[wsID] < 2 {
		t.Fatal("ожидали Resume или повторный Start")
	}

	// delete → записи нет, данные с диска убраны (решение владельца
	// 2026-10-14: удаление чистит bare-репку, клон и дом dsh)
	if code, _ := e.do(e.admin, "DELETE", "/api/workspaces/"+wsID, nil); code != http.StatusAccepted {
		t.Fatalf("delete: %d", code)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := e.db.WorkspaceByID(wsID); err == store.ErrNotFound {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, err := e.db.WorkspaceByID(wsID); err != store.ErrNotFound {
		t.Fatal("воркспейс не удалён из БД")
	}
	diskDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(diskDeadline) {
		if _, err := os.Stat(paths.Repo); os.IsNotExist(err) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	for _, p := range []string{paths.Repo, paths.Work, paths.DshHome} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("данные воркспейса должны быть удалены с диска: %s (%v)", p, err)
		}
	}

	// аудит: ключевые события зафиксированы
	events, _ := e.db.AuditList(100)
	seen := map[string]bool{}
	for _, ev := range events {
		seen[ev.Action] = true
	}
	for _, want := range []string{"register", "workspace.create", "workspace.running", "workspace.stop", "workspace.delete"} {
		if !seen[want] {
			t.Errorf("в аудите нет события %q", want)
		}
	}
}

// TestWorkspaceCreateValidation — некорректные параметры создания отклоняются.
func TestWorkspaceCreateValidation(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")

	bad := []map[string]any{
		{"name": "x", "baseURL": "https://l/v1", "models": []string{"m"}, "apiKey": "sk"},                              // короткое имя
		{"name": "ok-name", "template": "no-such", "baseURL": "https://l/v1", "models": []string{"m"}, "apiKey": "sk"}, // нет шаблона
		{"name": "ok-name", "baseURL": "litellm.example/v1", "models": []string{"m"}, "apiKey": "sk"},                  // без схемы
		{"name": "ok-name", "baseURL": "https://l/v1", "models": []string{}, "apiKey": "sk"},                           // без моделей
		{"name": "ok-name", "baseURL": "https://l/v1", "models": []string{"m"}},                                        // без токена
	}
	for i, req := range bad {
		if code, body := e.do(e.admin, "POST", "/api/workspaces", req); code != 400 {
			t.Fatalf("кейс %d: %d %s, ждали 400", i, code, body)
		}
	}
	// корректный запрос проходит; ждём завершения фонового provision,
	// чтобы временный каталог не разбирали под работающей горутиной
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("good"))
	if code != http.StatusAccepted {
		t.Fatalf("валидное создание отклонено: %d %s", code, body)
	}
	e.waitStatus(wsIDof(t, body), "running")
}

// TestWorkspaceQuota — лимит воркспейсов на пользователя соблюдается.
func TestWorkspaceQuota(t *testing.T) {
	e := newTestEnv(t, 2)
	e.register(e.admin, "anna", "secret1")
	for i := 0; i < 2; i++ {
		code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate(fmt.Sprintf("ws-%d", i)))
		if code != http.StatusAccepted {
			t.Fatalf("ws-%d: %d %s", i, code, body)
		}
		e.waitStatus(wsIDof(t, body), "running")
	}
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("third"))
	if code != 400 || !strings.Contains(body, "лимит") {
		t.Fatalf("третий воркспейс: %d %s, ждали 400 «лимит»", code, body)
	}
}

// TestWorkspaceIsolation — чужой воркспейс недоступен, админу виден всё.
func TestWorkspaceIsolation(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1") // админ
	bob := e.newClient()
	e.register(bob, "bob", "secret1")

	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("annas-ws"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	e.waitStatus(wsID, "running")

	// bob не видит воркспейс anny (403 «не ваш» — существование не скрывается,
	// но доступ закрыт; wsID непредсказуем, поэтому перебор невозможен)
	if code, _ := e.do(bob, "GET", "/api/workspaces/"+wsID, nil); code != 403 {
		t.Fatalf("чужой GET: %d, ждали 403", code)
	}
	if code, _ := e.do(bob, "POST", "/api/workspaces/"+wsID+"/stop", nil); code != 403 {
		t.Fatalf("чужой stop: %d, ждали 403", code)
	}
	if code, _ := e.do(bob, "DELETE", "/api/workspaces/"+wsID, nil); code != 403 {
		t.Fatalf("чужой delete: %d, ждали 403", code)
	}
	// в списке bob — пусто
	_, body = e.do(bob, "GET", "/api/workspaces", nil)
	if strings.Contains(body, wsID) {
		t.Fatal("воркспейс anny виден в списке bob")
	}
	// админ видит
	_, body = e.do(e.admin, "GET", "/api/workspaces", nil)
	if !strings.Contains(body, wsID) {
		t.Fatal("админ не видит воркспейс в списке всех")
	}
}

// TestWorkspaceDockerFail — ошибка рантайма переводит воркспейс в failed.
func TestWorkspaceDockerFail(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	e.rt.failNext = fmt.Errorf("docker: no such image")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("doomed"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "failed")
	if !strings.Contains(ws.Error, "docker") {
		t.Fatalf("ошибка статуса без упоминания docker: %q", ws.Error)
	}
	events, _ := e.db.AuditList(50)
	found := false
	for _, ev := range events {
		if ev.Action == "workspace.fail" {
			found = true
		}
	}
	if !found {
		t.Fatal("workspace.fail не попал в аудит")
	}
}

// TestProviderPatch — смена моделей без пересоздания: settings.yaml обновляется,
// токен в ответе не светится, при running статус уходит в provisioning.
func TestProviderPatch(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("patched"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "running")
	paths := provision.Paths(e.home, "anna", ws.ID, ws.Name)

	code, body = e.do(e.admin, "PATCH", "/api/workspaces/"+wsID+"/provider", map[string]any{
		"models": []string{"new-model-x"},
	})
	if code != 200 {
		t.Fatalf("patch: %d %s", code, body)
	}
	if strings.Contains(body, "sk-it-test-token") {
		t.Fatal("токен LLM утёк в ответе PATCH")
	}
	data, err := os.ReadFile(filepath.Join(paths.DshHome, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "new-model-x") {
		t.Fatal("settings.yaml не обновлён новой моделью")
	}
	// воркспейс был running → контейнер пересоздаётся → снова running
	e.waitStatus(wsID, "running")
	fresh, _ := e.db.WorkspaceByID(wsID)
	if fresh.Model != "new-model-x" {
		t.Fatalf("модель в БД = %q, ждали new-model-x", fresh.Model)
	}
}

// TestKitsEndpointAndSelection — версия манифеста и selected components доступны
// в API/SQLite, а снятая галочка не создаёт файлы компонента.
func TestKitsEndpointAndSelection(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	if code, _ := e.do(&http.Client{}, "GET", "/api/kits", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/kits: %d", code)
	}
	code, body := e.do(e.admin, "GET", "/api/kits", nil)
	if code != http.StatusOK || !strings.Contains(body, `"version":"1.0.0"`) || !strings.Contains(body, "1c-tooling") {
		t.Fatalf("kits: %d %s", code, body)
	}

	req := validCreate("components")
	req["kit"] = "1c-vibe"
	// Порядок JSON произвольный; сервер нормализует по manifest.
	req["components"] = []string{"core-instructions", "core-workspace"}
	code, body = e.do(e.admin, "POST", "/api/workspaces", req)
	if code != http.StatusAccepted {
		t.Fatalf("create selected kit: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "running")
	if got := strings.Join(ws.Components, ","); got != "core-workspace,core-instructions" {
		t.Fatalf("persisted components=%q", got)
	}
	if _, err := os.Stat(filepath.Join(ws.WorkPath, "AGENTS.md")); err != nil {
		t.Fatalf("core instructions missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws.WorkPath, "scripts/jira_client.py")); !os.IsNotExist(err) {
		t.Fatalf("deselected 1C tooling was copied, stat err=%v", err)
	}
}

// TestKitRejectsInvalidSelection — сервер запрещает пустой и чужой component ID.
func TestKitRejectsInvalidSelection(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	for _, components := range [][]string{{}, {"not-in-this-kit"}} {
		req := validCreate("invalid-kit")
		req["components"] = components
		code, _ := e.do(e.admin, "POST", "/api/workspaces", req)
		if code != http.StatusBadRequest {
			t.Fatalf("components=%v: got %d, want 400", components, code)
		}
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

// TestEntryRedirectRequiresRunning — /w/{id} ведёт на канонический /w/{id}/
// (шлюз) и не отвечает для не-running воркспейса.
func TestRedirectRequiresRunning(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("redir"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d", code)
	}
	wsID := wsIDof(t, body)
	e.waitStatus(wsID, "running")

	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"?token=should-not-leak&view=full", nil)
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("/w/{id}: %d, ждали 308", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/"+wsID+"/?view=full" {
		t.Fatalf("Location %q — токен не должен сохраняться, query view должен остаться", loc)
	}

	// не-running: останавливаем и ждём 409 от шлюза
	if code, _ := e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/stop", nil); code != 200 && code != 202 {
		t.Fatalf("stop: %d", code)
	}
	e.waitStatus(wsID, "stopped")
	req, _ = http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err = e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("/w/{id}/ stopped: %d, ждали 409", resp.StatusCode)
	}
}

// TestWsTokenTakesLastMatch — регрессия баг-фикса: при docker start того же
// контейнера в журнале накапливаются несколько boot-строк; сервер обязан
// отдавать ПОСЛЕДНИЙ токен (первый к этому моменту устарел — dsh печатает
// новый при каждом старте, старый даёт 401).
func TestWsTokenTakesLastMatch(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("retok"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	e.waitStatus(wsID, "running")

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

// TestWorkspaceNameReuse — с 2026-10-14 удаление воркспейса чистит диск,
// поэтому то же имя свободно сразу; отдельный случай — брошенные на диске
// файлы от воркспейсов, удалённых старой версией: повторное имя даёт
// понятную ошибку, а не падение provision на «bare init».
func TestWorkspaceNameReuse(t *testing.T) {
	e := newTestEnv(t, 5)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("twice"))
	if code != http.StatusAccepted {
		t.Fatalf("create #1: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	e.waitStatus(wsID, "running")
	paths := provision.Paths(e.home, "anna", wsID, "twice")
	if code, _ := e.do(e.admin, "DELETE", "/api/workspaces/"+wsID, nil); code != http.StatusAccepted {
		t.Fatal("delete")
	}
	// ждём, пока фоновая уборка уберёт файлы с диска
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(paths.Repo); os.IsNotExist(err) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, err := os.Stat(paths.Repo); !os.IsNotExist(err) {
		t.Fatalf("репка удалённого воркспейса осталась на диске: %v", err)
	}

	// то же имя теперь свободно
	code, body = e.do(e.admin, "POST", "/api/workspaces", validCreate("twice"))
	if code != http.StatusAccepted {
		t.Fatalf("повторное имя: %d %s, ждали 202", code, body)
	}
	e.waitStatus(wsIDof(t, body), "running")

	// страховка для остатков от удаления старой версии: занятая репка — 400
	leftover := provision.Paths(e.home, "anna", "w-legacy000000", "twice")
	if err := os.MkdirAll(leftover.Repo, 0o755); err != nil {
		t.Fatal(err)
	}
	code, body = e.do(e.admin, "POST", "/api/workspaces", validCreate("twice"))
	if code != 400 || !strings.Contains(body, "уже есть на диске") {
		t.Fatalf("занятое имя: %d %s, ждали 400 с понятной ошибкой", code, body)
	}
}
