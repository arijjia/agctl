package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

// TestChatDefaultLLMPersistsAcrossRestarts — платформенный провайдер, заданный
// один раз (флаги/env), восстанавливается из зашифрованных настроек после
// рестарта без AGCTL_LLM_*: модал dsh «Add an API key» не возвращается.
func TestChatDefaultLLMPersistsAcrossRestarts(t *testing.T) {
	home := t.TempDir()
	build := func(tune func(*Config)) (*Server, *fakeRuntime, func()) {
		db, err := store.Open(filepath.Join(home, "test.db"))
		if err != nil {
			t.Fatal(err)
		}
		keyPath, err := secrets.EnsureMasterKey(home)
		if err != nil {
			t.Fatal(err)
		}
		sec, err := secrets.New(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		rt := newFakeRuntime()
		rt.chatHome = home
		cfg := Config{Home: home, Addr: "test", MaxPerUser: 3, AutosaveEvery: time.Hour}
		if tune != nil {
			tune(&cfg)
		}
		s, err := NewWithRuntimeProviders(db, sec, cfg, rt, rt)
		if err != nil {
			t.Fatal(err)
		}
		return s, rt, func() { s.Close(); db.Close() }
	}

	_, _, close1 := build(func(c *Config) {
		c.DefaultLLM = provision.Provider{
			BaseURL: "https://corp.example.invalid/v1", Model: "qwen-3.8-fast", APIKey: "persist-key-1",
		}
	})
	close1() // рестарт: тот же home, но без DefaultLLM в флагах

	s2, rt2, close2 := build(nil)
	defer close2()
	if _, err := s2.ensureChat(505); err != nil {
		t.Fatal(err)
	}
	got := rt2.chatSpecs[s2.chatRuntimeID(505)].Provider
	if got.BaseURL != "https://corp.example.invalid/v1" || got.Model != "qwen-3.8-fast" || got.APIKey != "persist-key-1" {
		t.Fatalf("restored provider = %+v; want persisted corporate provider", got)
	}
}

func TestEnsureChatIsPerUserAndReusesRuntime(t *testing.T) {
	e := newTestEnv(t, 3)
	one, err := e.srv.ensureChat(101)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.srv.ensureChat(101)
	if err != nil {
		t.Fatal(err)
	}
	two, err := e.srv.ensureChat(202)
	if err != nil {
		t.Fatal(err)
	}
	if one != again {
		t.Fatal("same user should reuse one live chat runtime")
	}
	if one.handle.RuntimeID == two.handle.RuntimeID || one.handle.Endpoint == two.handle.Endpoint {
		t.Fatalf("users share runtime unexpectedly: one=%+v two=%+v", one.handle, two.handle)
	}
	home1 := filepath.Join(e.home, "chats", "u101", "dsh-home")
	home2 := filepath.Join(e.home, "chats", "u202", "dsh-home")
	if home1 == home2 {
		t.Fatalf("users share persistent DSH_HOME: %s", home1)
	}
	otherDeployment := &Server{cfg: Config{Home: t.TempDir()}}
	if e.srv.chatRuntimeID(101) == otherDeployment.chatRuntimeID(101) {
		t.Fatal("separate agctl homes must not collide in runtime ids")
	}
	if _, err := os.Stat(filepath.Join(home1, "profiles", "web", "package.json")); err != nil {
		t.Fatalf("user volume not seeded: %v", err)
	}
	if e.rt.chatStarts[e.srv.chatRuntimeID(101)] != 1 || e.rt.chatStarts[e.srv.chatRuntimeID(202)] != 1 {
		t.Fatalf("chat start counts: %+v", e.rt.chatStarts)
	}
}

func TestReaperStopsIdleChatButKeepsPersistentVolume(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Second
	chat, err := e.srv.ensureChat(303)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(e.home, "chats", "u303")
	home, work := filepath.Join(base, "dsh-home"), filepath.Join(base, "work")
	chat.lastSeen = time.Now().Add(-2 * time.Second)
	e.srv.reapIdleChats()
	if e.srv.chats[303] != nil {
		t.Fatal("idle runtime remains registered")
	}
	if e.rt.chatStops[chat.handle.RuntimeID] != 1 {
		t.Fatalf("chat stop count = %d, want 1", e.rt.chatStops[chat.handle.RuntimeID])
	}
	for _, dir := range []string{home, work} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("persistent volume path %s was removed: %v", dir, err)
		}
	}
}

func TestChatUsesPlatformDefaultProviderWhenNoPersonalSecret(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.DefaultLLM = provision.Provider{
		BaseURL: "https://llm.example.invalid/v1", Model: "qwen-3.8-fast",
		Models: []string{"qwen-3.8-fast", "qwen-3.8-27b"}, APIKey: "platform-key-xyz",
	}
	if _, err := e.srv.ensureChat(606); err != nil {
		t.Fatal(err)
	}
	got := e.rt.chatSpecs[e.srv.chatRuntimeID(606)].Provider
	if got.BaseURL != e.srv.cfg.DefaultLLM.BaseURL || got.APIKey != "platform-key-xyz" || len(got.Models) != 2 {
		t.Fatalf("chat provider = %+v, want platform default", got)
	}
	if got.Model != "qwen-3.8-fast" {
		t.Fatalf("default model = %q, want qwen-3.8-fast", got.Model)
	}
}

func TestPersonalSecretOverridesPlatformDefault(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "https://corp.example.invalid/v1", Model: "corp-model", APIKey: "corp-key"}
	cipher, err := e.srv.sec.Encrypt("personal-key")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := e.srv.db.CreateUser("provider-user", "h", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.srv.db.SecretUpsert(uid, "https://personal.example.invalid/v1", "my-model", []string{"my-model"}, cipher); err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.ensureChat(uid); err != nil {
		t.Fatal(err)
	}
	got := e.rt.chatSpecs[e.srv.chatRuntimeID(uid)].Provider
	if got.BaseURL != "https://personal.example.invalid/v1" || got.APIKey != "personal-key" || got.Model != "my-model" {
		t.Fatalf("chat provider = %+v, want personal secret to win", got)
	}
}

func TestChatEnsureRefreshesProviderOnRestartedRuntimeHome(t *testing.T) {
	e := newTestEnv(t, 3)
	// DSH_HOME, засеянный старой версией agctl без платформенного провайдера.
	home := filepath.Join(e.home, "chats", "u707", "dsh-home")
	if err := provision.SeedDshHome(home, provision.Provider{}, assetsDshSkel); err != nil {
		t.Fatal(err)
	}
	e.srv.cfg.DefaultLLM = provision.Provider{
		BaseURL: "https://corp.example.invalid/v1", Model: "qwen-3.8-fast", APIKey: "corp-key",
	}
	if _, err := e.srv.ensureChat(707); err != nil {
		t.Fatal(err)
	}
	creds, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(creds), "AGCTL_LLM_API_KEY: ") {
		t.Fatalf("restarted home was not refreshed with platform provider: %s", creds)
	}
}

func TestChatTokenRefreshesAfterPodOrContainerRestart(t *testing.T) {
	e := newTestEnv(t, 3)
	chat, err := e.srv.ensureChat(505)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.srv.chatToken(context.Background(), chat.handle); got != "fake-chat-"+chat.handle.RuntimeID {
		t.Fatalf("initial boot token = %q", got)
	}
	e.rt.logs[chat.handle.InstanceID] = "dsh web listening on http://127.0.0.1/?token=refreshed-token"
	if got := e.srv.chatToken(context.Background(), chat.handle); got != "refreshed-token" {
		t.Fatalf("boot token after restart = %q, want refreshed token", got)
	}
}

func TestReaperDoesNotStopActiveChat(t *testing.T) {
	e := newTestEnv(t, 3)
	e.srv.cfg.ChatTTL = time.Second
	chat, err := e.srv.ensureChat(404)
	if err != nil {
		t.Fatal(err)
	}
	chat.lastSeen = time.Now().Add(-time.Hour)
	chat.active = 1
	e.srv.reapIdleChats()
	if e.srv.chats[404] == nil || e.rt.chatStops[chat.handle.RuntimeID] != 0 {
		t.Fatal("reaper stopped an active chat request")
	}
}

// getChatHTML — GET /chat/ с браузерным Accept: text/html (страница чата).
func (e *testEnv) getChatHTML(c *http.Client) (int, string) {
	e.t.Helper()
	req, err := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

// TestChatProxyRecreatesRuntimeAfterDowntime — после смерти контейнера вне
// жизненного цикла серва (рестарт Docker VM, ручной docker rm) кэшированный
// endpoint не должен вечно показывать «Личный чат запускается…»: dial-отказы
// внутри grace считаются нормальным прогревом dsh, дольше grace — кэш
// сбрасывается и /chat/ лениво пересоздаёт рантайм.
func TestChatProxyRecreatesRuntimeAfterDowntime(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true }) // маршруты /chat/
	e.register(e.admin, "anna", "secret1")
	uid := int64(1) // первый зарегистрированный пользователь в чистой test-db

	chat, err := e.srv.ensureChat(uid)
	if err != nil {
		t.Fatal(err)
	}
	// Фейковый endpoint — мёртвый порт: имитируем погибший контейнер.
	if got := chat.handle.Endpoint; got != "http://127.0.0.1:40001" {
		t.Fatalf("fixture endpoint = %q, want dead fake port", got)
	}

	// 1) Первый dial-отказ: страница прогрева, кэш ЖИВ (не пересоздаём —
	// так работает и нормальный первый прогрев dsh).
	code, body := e.getChatHTML(e.admin)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "запускается") {
		t.Fatalf("warm page: code=%d body=%q", code, body)
	}
	if e.rt.chatStarts[chat.handle.RuntimeID] != 1 {
		t.Fatalf("grace period must not recreate runtime, starts = %d", e.rt.chatStarts[chat.handle.RuntimeID])
	}
	e.srv.chatMu.Lock()
	marked := !e.srv.chats[uid].downSince.IsZero()
	e.srv.chatMu.Unlock()
	if !marked {
		t.Fatal("first dial failure did not mark runtime down")
	}

	// 2) Grace истёк: следующий отказ выселяет кэш (сам запрос ещё получает
	// страницу прогрева — как браузер по мета-refresh), а запрос после
	// вытеснения пересоздаёт рантайм на живом endpoint.
	e.srv.chatMu.Lock()
	e.srv.chats[uid].downSince = time.Now().Add(-2 * chatDownGrace)
	e.srv.chatMu.Unlock()
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "chat-is-alive")
	}))
	defer live.Close()
	e.rt.chatEndpointOverride = live.URL

	code, body = e.getChatHTML(e.admin)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "запускается") {
		t.Fatalf("eviction request: code=%d body=%q", code, body)
	}
	e.srv.chatMu.Lock()
	evicted := e.srv.chats[uid] == nil
	e.srv.chatMu.Unlock()
	if !evicted {
		t.Fatal("runtime older than grace was not evicted")
	}

	// 3) Как следующий авто-refresh браузера: рантайм пересоздан, прокси живой.
	code, body = e.getChatHTML(e.admin)
	if code != http.StatusOK || !strings.Contains(body, "chat-is-alive") {
		t.Fatalf("recovered proxy: code=%d body=%q", code, body)
	}
	if e.rt.chatStarts[chat.handle.RuntimeID] != 2 {
		t.Fatalf("runtime must be recreated exactly once more, starts = %d", e.rt.chatStarts[chat.handle.RuntimeID])
	}
	e.srv.chatMu.Lock()
	healthy := e.srv.chats[uid] != nil && e.srv.chats[uid].downSince.IsZero()
	e.srv.chatMu.Unlock()
	if !healthy {
		t.Fatal("recreated runtime must be cached and healthy")
	}
}
