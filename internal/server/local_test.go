package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/provision"
)

func newLocalEnv(t *testing.T) (*testEnv, *LocalMode) {
	t.Helper()
	work := t.TempDir()
	lm := &LocalMode{
		OwnerLogin: "owner", WorkDir: work,
		LoginToken: "login-tok", ControlToken: "ctl-tok", BridgeToken: "br-tok",
		BridgeURL: "http://host.docker.internal:8930",
		BridgeTools: []localbridge.Tool{{
			Name: "go_env", Description: "go env", Command: []string{"go", "env", "{{name}}"},
			Args: map[string]localbridge.Arg{"name": {Required: true, Pattern: `[A-Z]+`}},
		}},
		Env: []string{"GITLAB_TOKEN=gl-tok"},
	}
	env := newTestEnvWith(t, 1, func(c *Config) { c.Local = lm; c.Tier0 = true })
	t.Cleanup(func() { provision.SetLocalWorkRoot(env.home, env.srv.local.ownerID, "") })
	return env, lm
}

func (e *testEnv) bearer(method, path, token string, body string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.api.URL+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	b := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	return resp.StatusCode, buf.String()
}

func TestLocalLoginOwnerAndNoRegistration(t *testing.T) {
	env, _ := newLocalEnv(t)
	c := env.newClient()
	if code, _ := env.do(c, "POST", "/api/register", map[string]string{"login": "x", "password": "Password-123!"}); code != http.StatusForbidden {
		t.Fatalf("регистрация в локальном режиме: %d", code)
	}
	if code, _ := env.do(c, "GET", "/local/login?token=wrong", nil); code != http.StatusUnauthorized {
		t.Fatalf("неверный токен входа: %d", code)
	}
	code, _ := env.do(c, "GET", "/local/login?token=login-tok", nil)
	if code != http.StatusSeeOther {
		t.Fatalf("вход владельца: %d", code)
	}
	code, body := env.do(c, "GET", "/api/me", nil)
	if code != 200 || !strings.Contains(body, `"role":"developer"`) || !strings.Contains(body, `"owner"`) {
		t.Fatalf("/api/me: %d %s", code, body)
	}
	// вход прогревает рабочее место владельца с родной папкой и env
	env.waitLocalWarm()
	var spec = env.rt.chatSpecs[env.srv.chatRuntimeID(env.srv.local.ownerID)]
	if spec.WorkDir == "" || !strings.Contains(strings.Join(spec.DevEnv, ","), "GITLAB_TOKEN=gl-tok") ||
		!strings.Contains(strings.Join(spec.DevEnv, ","), "AGCTL_BRIDGE_TOKEN=br-tok") {
		t.Fatalf("spec рабочего места: workdir=%q env=%v", spec.WorkDir, spec.DevEnv)
	}
	skill := filepath.Join(env.srv.projectDshHome(env.srv.local.ownerID), "skills", "agctl-host-bridge", "SKILL.md")
	raw, err := os.ReadFile(skill)
	if err != nil || !strings.Contains(string(raw), "go_env") {
		t.Fatalf("скилл моста: %v %s", err, raw)
	}
}

func TestLocalControlAndBridgeAuth(t *testing.T) {
	env, _ := newLocalEnv(t)
	if code, _ := env.bearer("GET", "/api/local/status", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("status без токена: %d", code)
	}
	if code, _ := env.bearer("GET", "/api/local/status", "br-tok", ""); code != http.StatusUnauthorized {
		t.Fatal("токен моста не даёт управления")
	}
	code, body := env.bearer("GET", "/api/local/status", "ctl-tok", "")
	if code != 200 || !strings.Contains(body, `"bridgeTools":1`) {
		t.Fatalf("status: %d %s", code, body)
	}
	if code, _ := env.bearer("GET", localbridge.PathTools, "ctl-tok", ""); code != http.StatusUnauthorized {
		t.Fatal("токен управления не даёт моста")
	}
	code, body = env.bearer("GET", localbridge.PathTools, "br-tok", "")
	if code != 200 || !strings.Contains(body, "go_env") || strings.Contains(body, `"command"`) {
		t.Fatalf("tools: %d %s", code, body)
	}
	code, body = env.bearer("POST", localbridge.PathCall, "br-tok", `{"tool":"go_env","args":{"name":"GOOS"}}`)
	if code != 200 || !strings.Contains(body, `"exitCode":0`) {
		t.Fatalf("call: %d %s", code, body)
	}
	if code, _ := env.bearer("POST", localbridge.PathCall, "br-tok", `{"tool":"rm","args":{}}`); code != http.StatusBadRequest {
		t.Fatal("инструмент вне белого списка")
	}
}

func TestLocalProjectsUseWorkDirAndNeverDelete(t *testing.T) {
	env, lm := newLocalEnv(t)
	if err := os.Mkdir(filepath.Join(lm.WorkDir, "my-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := env.newClient()
	env.do(c, "GET", "/local/login?token=login-tok", nil)
	env.waitLocalWarm()
	code, body := env.do(c, "GET", "/api/projects", nil)
	if code != 200 || !strings.Contains(body, "my-repo") {
		t.Fatalf("проекты из родной папки: %d %s", code, body)
	}
	if code, _ := env.do(c, "DELETE", "/api/projects/my-repo", nil); code != http.StatusConflict {
		t.Fatalf("удаление папки из UI в локальном режиме: %d", code)
	}
	if _, err := os.Stat(filepath.Join(lm.WorkDir, "my-repo")); err != nil {
		t.Fatal("папка разработчика не должна удаляться")
	}
}

func TestServerModeHasNoLocalRoutes(t *testing.T) {
	env := newTestEnv(t, 1)
	if code, _ := env.bearer("GET", "/api/local/status", "x", ""); code == 200 {
		t.Fatal("в серверном режиме ручек локального режима нет")
	}
	if code, _ := env.do(env.newClient(), "GET", "/local/login?token=x", nil); code == http.StatusSeeOther {
		t.Fatal("в серверном режиме входа по токену нет")
	}
}

// waitLocalWarm ждёт фонового прогрева рабочего места после входа (скилл
// моста пишется последним шагом ensureChat).
func (e *testEnv) waitLocalWarm() {
	e.t.Helper()
	skill := filepath.Join(e.srv.projectDshHome(e.srv.local.ownerID), "skills", "agctl-host-bridge", "SKILL.md")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.srv.chatMu.Lock()
		rt := e.srv.chats[e.srv.local.ownerID]
		e.srv.chatMu.Unlock()
		if rt != nil {
			if _, err := os.Stat(skill); err == nil {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatal("рабочее место владельца не прогрелось")
}

func TestHostBridgeMcpOnlyInLocalMode(t *testing.T) {
	srv := newTestEnv(t, 1)
	c := srv.newClient()
	srv.do(c, "POST", "/api/register", map[string]string{"login": "dev1", "password": "Password-123!"})
	if err := os.MkdirAll(filepath.Join(srv.home, "chats", "u1", "work", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, body := srv.do(c, "GET", "/api/projects/demo/mcp", nil)
	if strings.Contains(body, "host-bridge") {
		t.Fatalf("мост к хосту не показывается в серверном режиме: %s", body)
	}
	if code, _ := srv.do(c, "PUT", "/api/projects/demo/mcp", map[string]any{"servers": []string{"host-bridge"}}); code != http.StatusBadRequest {
		t.Fatalf("выбор моста в серверном режиме отклоняется: %d", code)
	}

	env, lm := newLocalEnv(t)
	_ = os.Mkdir(filepath.Join(lm.WorkDir, "demo"), 0o755)
	lc := env.newClient()
	env.do(lc, "GET", "/local/login?token=login-tok", nil)
	env.waitLocalWarm()
	_, body = env.do(lc, "GET", "/api/projects/demo/mcp", nil)
	if !strings.Contains(body, "host-bridge") {
		t.Fatalf("локальный режим показывает мост: %s", body)
	}
	if code, b := env.do(lc, "PUT", "/api/projects/demo/mcp", map[string]any{"servers": []string{"host-bridge", "gitlab"}}); code != 200 {
		t.Fatalf("PUT: %d %s", code, b)
	}
	raw, err := os.ReadFile(filepath.Join(env.srv.projectDshHome(env.srv.local.ownerID), ".agent-presets", "agctl-ws-demo", "agent.cordis.yml"))
	if err != nil || !strings.Contains(string(raw), "AGCTL_BRIDGE_TOKEN: br-tok") {
		t.Fatalf("пресет моста: %v\n%s", err, raw)
	}
}
