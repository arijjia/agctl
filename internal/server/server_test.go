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
	// chatHeaders — заголовки handle (адаптеры с общим Endpoint: Substrate).
	chatHeaders map[string]string
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
		Endpoint: endpoint, SSHPort: sshPort, Headers: f.chatHeaders,
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
	return newTestEnvWithRuntime(t, maxPerUser, tune, nil)
}

// newTestEnvWithRuntime — как newTestEnvWith, но с подменным рантаймом
// (например, реализующим containerLister для тестов контейнеров/логов).
func newTestEnvWithRuntime(t *testing.T, maxPerUser int, tune func(*Config), rt RuntimeIface) *testEnv {
	t.Helper()
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "test.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	// Close регистрируем сразу: если конструирование ниже запаникует,
	// Windows не сможет удалить открытый test.db в cleanup t.TempDir.
	t.Cleanup(func() { _ = db.Close() })
	keyPath, err := secrets.EnsureMasterKey(home)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	fake := newFakeRuntime()
	fake.chatHome = home
	if rt == nil {
		rt = fake
	}
	cfg := Config{Home: home, Addr: "test", MaxPerUser: maxPerUser, AutosaveEvery: time.Hour}
	if tune != nil {
		tune(&cfg)
	}
	if cfg.Modules == nil && cfg.Local == nil {
		cfg.Modules = ServerModules()
	}
	chatProvider, _ := rt.(agctlrt.ChatProvider)
	if chatProvider == nil {
		chatProvider = fake
	}
	s, err := NewWithRuntimeProviders(db, sec, cfg, rt, chatProvider)
	if err != nil {
		t.Fatalf("server runtime injection: %v", err)
	}
	// минимальный веб-корень для статики SPA
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>agctl</title>")}}
	api := httptest.NewServer(s.Routes(webFS))
	t.Cleanup(api.Close)
	env := &testEnv{t: t, srv: s, api: api, rt: fake, home: home, db: db}
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

// ── воркспейсы: полный жизненный цикл ───────────────────────────────────────

// TestWsTokenTakesLastMatch — регрессия баг-фикса: при docker start того же
// контейнера в журнале накапливаются несколько boot-строк; сервер обязан
// отдавать ПОСЛЕДНИЙ токен (первый к этому моменту устарел — dsh печатает
// новый при каждом старте, старый даёт 401). В v3 то же правило живёт в
// chatToken/cachedBootToken для чат-рантаймов.
func TestWsTokenTakesLastMatch(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	handle := agctlrt.ChatHandle{
		RuntimeID:  e.srv.chatRuntimeID(2, 0),
		InstanceID: "retok-inst",
		Endpoint:   "http://127.0.0.1:43210",
	}
	// эмулируем рестарт контейнера: в журнале две boot-строки
	e.rt.logs[handle.InstanceID] = "dsh web: http://0.0.0.0:3080/?token=OLD-stale-token\n" +
		"dsh web: http://0.0.0.0:3080/?token=NEW-fresh-token"
	e.srv.forgetToken(chatTokenCacheKey(handle))

	tok := e.srv.chatToken(context.Background(), handle)
	if tok != "NEW-fresh-token" {
		t.Fatalf("chatToken взял устаревший токен: %q", tok)
	}
	// и кэш теперь хранит свежий токен
	e.srv.tokMu.Lock()
	cached := e.srv.tokens[chatTokenCacheKey(handle)]
	e.srv.tokMu.Unlock()
	if cached != "NEW-fresh-token" {
		t.Fatalf("в кэше %q, ждали NEW-fresh-token", cached)
	}
}
