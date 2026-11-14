package server

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/localmode"
)

// fakeHooks — хост лаунчера в памяти: секреты, образы, «сборка».
type fakeHooks struct {
	mu      sync.Mutex
	vals    map[string]string
	plain   map[string]bool
	image   string
	present map[string]bool
	built   []string
}

func newFakeHooks() *fakeHooks {
	return &fakeHooks{
		vals: map[string]string{}, plain: map[string]bool{},
		image:   "agctl-dsh-org:0.2.2",
		present: map[string]bool{"agctl-dsh-org:0.2.2": true},
	}
}

func (f *fakeHooks) Entries() []localmode.Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []localmode.Entry
	for _, v := range localmode.Manifest {
		e := localmode.Entry{Name: v.Name, Title: v.Title, Group: v.Group, Secret: v.Secret, Known: true}
		if val := f.vals[v.Name]; val != "" {
			e.Set = true
			if !v.Secret {
				e.Value = val
			}
		}
		out = append(out, e)
	}
	return out
}
func (f *fakeHooks) SetValue(name, value string, plain bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vals[name], f.plain[name] = value, plain
	return nil
}
func (f *fakeHooks) DeleteValue(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vals, name)
	return nil
}
func (f *fakeHooks) ContainerEnv() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var env []string
	for k, v := range f.vals {
		if k != "AGCTL_LLM_API_KEY" {
			env = append(env, k+"="+v)
		}
	}
	return env
}
func (f *fakeHooks) LLMKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.vals["AGCTL_LLM_API_KEY"]
}
func (f *fakeHooks) StoreBackend() string { return "тестовое хранилище" }
func (f *fakeHooks) Images() []LocalImage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []LocalImage{
		{Ref: "agctl-dsh-org:0.2.2", Present: f.present["agctl-dsh-org:0.2.2"]},
		{Ref: "agctl-dsh-org-1c:0.2.2", Present: f.present["agctl-dsh-org-1c:0.2.2"], Flavor: "1c"},
	}
}
func (f *fakeHooks) CurrentImage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.image
}
func (f *fakeHooks) SetImage(ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.image = ref
	return nil
}
func (f *fakeHooks) EnsureImage(ctx context.Context, ref string, logf func(string)) error {
	logf("собираю " + ref)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.built = append(f.built, ref)
	f.present[ref] = true
	return nil
}

func newLocalUIEnv(t *testing.T) (*testEnv, *fakeHooks) {
	t.Helper()
	h := newFakeHooks()
	env, lm := newLocalEnv(t)
	lm.Hooks = h
	// initLocal уже отработал без хуков — образ-источник правды задаём как при старте
	if err := env.srv.db.SettingSet(chatImageSettingKey(env.srv.local.ownerID), h.CurrentImage()); err != nil {
		t.Fatal(err)
	}
	return env, h
}

func (e *testEnv) waitTask(want string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := e.srv.local.task.snapshot()
		if snap["state"] == want {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("задача не дошла до %q: %v", want, e.srv.local.task.snapshot())
	return nil
}

func TestLocalLauncherLoginAndSecrets(t *testing.T) {
	env, h := newLocalUIEnv(t)
	c := env.newClient()
	// без ключа модели вход ведёт в лаунчер и рабочее место не греется
	if code, _ := env.do(c, "GET", "/local/login?token=login-tok", nil); code != http.StatusSeeOther {
		t.Fatalf("вход: %d", code)
	}
	if snap := env.srv.local.task.snapshot(); snap["state"] != "idle" {
		t.Fatalf("без ключа модели старт не должен начинаться: %v", snap)
	}
	code, body := env.do(c, "GET", "/api/local/ui", nil)
	if code != 200 || !strings.Contains(body, `"llmKeySet":false`) || !strings.Contains(body, "agctl-dsh-org-1c:0.2.2") {
		t.Fatalf("/api/local/ui: %d %s", code, body)
	}
	// секрет: сохраняется, значение наружу не отдаётся
	if code, body := env.do(c, "PUT", "/api/local/ui/secrets/GITLAB_TOKEN", map[string]any{"value": "glpat-SECRET-123"}); code != 200 {
		t.Fatalf("set secret: %d %s", code, body)
	}
	if code, body := env.do(c, "PUT", "/api/local/ui/secrets/AGCTL_LLM_API_KEY", map[string]any{"value": "llm-KEY-456"}); code != 200 {
		t.Fatalf("set llm key: %d %s", code, body)
	}
	_, body = env.do(c, "GET", "/api/local/ui", nil)
	if strings.Contains(body, "glpat-SECRET-123") || strings.Contains(body, "llm-KEY-456") {
		t.Fatalf("значение секрета утекло в ответ: %s", body)
	}
	if !strings.Contains(body, `"llmKeySet":true`) {
		t.Fatalf("ключ модели не отмечен заданным: %s", body)
	}
	if env.srv.defaultLLM().APIKey != "llm-KEY-456" {
		t.Fatalf("ключ модели не попал в провайдер сервера")
	}
	// несекретное значение видно
	env.do(c, "PUT", "/api/local/ui/secrets/GITLAB_URL", map[string]any{"value": "https://git.example.org"})
	if _, body = env.do(c, "GET", "/api/local/ui", nil); !strings.Contains(body, "https://git.example.org") {
		t.Fatalf("несекретное значение не видно: %s", body)
	}
	// удаление
	if code, _ := env.do(c, "DELETE", "/api/local/ui/secrets/GITLAB_TOKEN", nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if h.vals["GITLAB_TOKEN"] != "" {
		t.Fatal("секрет не удалён")
	}
}

func TestLocalLauncherCrossOriginAndAnon(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	if code, _ := env.do(env.newClient(), "GET", "/api/local/ui", nil); code != http.StatusUnauthorized {
		t.Fatalf("без сессии: %d", code)
	}
	c := env.newClient()
	env.do(c, "GET", "/local/login?token=login-tok", nil)
	req, _ := http.NewRequest("PUT", env.api.URL+"/api/local/ui/secrets/GITLAB_TOKEN", strings.NewReader(`{"value":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("чужой Origin: %d", resp.StatusCode)
	}
}

func TestLocalLauncherBuildsMissingImageThenStarts(t *testing.T) {
	env, h := newLocalUIEnv(t)
	c := env.newClient()
	env.do(c, "GET", "/local/login?token=login-tok", nil)
	if code, body := env.do(c, "PUT", "/api/local/ui/image", map[string]any{"image": "evil:latest"}); code != http.StatusBadRequest {
		t.Fatalf("образ вне каталога: %d %s", code, body)
	}
	if code, body := env.do(c, "PUT", "/api/local/ui/image", map[string]any{"image": "agctl-dsh-org-1c:0.2.2"}); code != 200 {
		t.Fatalf("выбор образа: %d %s", code, body)
	}
	if code, body := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatalf("старт: %d %s", code, body)
	}
	snap := env.waitTask("idle")
	if len(h.built) != 1 || h.built[0] != "agctl-dsh-org-1c:0.2.2" {
		t.Fatalf("недостающий образ не собран: %v", h.built)
	}
	if !strings.Contains(strings.Join(snap["log"].([]string), "\n"), "рабочее место запущено") {
		t.Fatalf("лог задачи: %v", snap["log"])
	}
	spec := env.rt.chatSpecs[env.srv.chatRuntimeID(env.srv.local.ownerID)]
	if spec.Image != "agctl-dsh-org-1c:0.2.2" {
		t.Fatalf("рабочее место не на выбранном образе: %q", spec.Image)
	}
	// смена доступа у работающего места → нужен перезапуск; restart его снимает
	env.do(c, "PUT", "/api/local/ui/secrets/JIRA_API_TOKEN", map[string]any{"value": "jira-tok"})
	if snap := env.srv.local.task.snapshot(); snap["restartNeeded"] != true {
		t.Fatalf("restartNeeded не выставлен: %v", snap)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/restart", nil); code != http.StatusAccepted {
		t.Fatalf("restart: %d", code)
	}
	env.waitTask("idle")
	if snap := env.srv.local.task.snapshot(); snap["restartNeeded"] != false {
		t.Fatalf("restartNeeded не снят: %v", snap)
	}
	spec = env.rt.chatSpecs[env.srv.chatRuntimeID(env.srv.local.ownerID)]
	if !strings.Contains(strings.Join(spec.DevEnv, ","), "JIRA_API_TOKEN=jira-tok") {
		t.Fatalf("новый доступ не попал в контейнер: %v", spec.DevEnv)
	}
}
