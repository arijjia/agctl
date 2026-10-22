package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/provision"
)

func TestRedactWorkspaceLogs(t *testing.T) {
	input := "url=http://127.0.0.1/?token=dsh-login-secret Authorization: Bearer bearer-secret AGCTL_LLM_API_KEY=llm-secret dsh-auth-cookie=cookie-secret api-key=sk-1234567890abcdef"
	got := redactWorkspaceLogs(input)
	for _, secret := range []string{"dsh-login-secret", "bearer-secret", "llm-secret", "cookie-secret", "sk-1234567890abcdef"} {
		if strings.Contains(got, secret) {
			t.Fatalf("log secret %q leaked after redaction: %s", secret, got)
		}
	}
	if strings.Count(got, "[redacted]") != 5 {
		t.Fatalf("redacted log = %q; expected 5 redactions", got)
	}
}

// Workspace-specific provider config must survive user-default changes and a
// stop/start cycle; the user-level secret is only the credential fallback.
func TestWorkspaceRestartKeepsWorkspaceProvider(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate("provider-stays"))
	if code != http.StatusAccepted {
		t.Fatalf("create: %d %s", code, body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "running")
	oldContainer := ws.ContainerID

	code, body = e.do(e.admin, "PATCH", "/api/workspaces/"+wsID+"/provider", map[string]any{
		"baseURL": "https://workspace-provider.example/v1",
		"models":  []string{"workspace-model"},
	})
	if code != http.StatusOK {
		t.Fatalf("provider patch: %d %s", code, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ws, _ = e.db.WorkspaceByID(wsID)
		if e.rt.started[wsID] >= 2 && e.rt.providers[wsID].BaseURL == "https://workspace-provider.example/v1" {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	patched := e.rt.providers[wsID]
	if ws.Status != "running" || e.rt.started[wsID] < 2 || patched.BaseURL != "https://workspace-provider.example/v1" {
		t.Fatalf("provider patch did not recreate runtime with workspace provider: status=%s starts=%d baseURL=%q models=%v", ws.Status, e.rt.started[wsID], patched.BaseURL, patched.Models)
	}

	if code, body = e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/stop", nil); code != http.StatusAccepted {
		t.Fatalf("stop: %d %s", code, body)
	}
	e.waitStatus(wsID, "stopped")

	user, err := e.db.UserByLogin("anna")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := e.srv.sec.Encrypt("sk-global-rotated-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.SecretUpsert(user.ID, "https://new-user-default.example/v1", "global-model", []string{"global-model"}, cipher); err != nil {
		t.Fatal(err)
	}

	if code, body = e.do(e.admin, "POST", "/api/workspaces/"+wsID+"/start", nil); code != http.StatusAccepted {
		t.Fatalf("start: %d %s", code, body)
	}
	ws = e.waitStatus(wsID, "running")
	got := e.rt.providers[wsID]
	if got.BaseURL != "https://workspace-provider.example/v1" || got.DefaultModel() != "workspace-model" {
		t.Fatalf("restart provider baseURL=%q model=%q; workspace config should survive owner default changes", got.BaseURL, got.DefaultModel())
	}
	if got.APIKey != "sk-global-rotated-token" {
		t.Fatalf("restart should use the current encrypted user token, got APIKey length=%d", len(got.APIKey))
	}
	if e.rt.started[wsID] < 3 {
		t.Fatalf("stopped workspace start did not recreate container, Start calls=%d (old ID=%s current=%s)", e.rt.started[wsID], oldContainer, ws.ContainerID)
	}
}

func TestWorkspaceMultipleBaseURLsAndSharedModels(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	payload := validCreate("multi-provider")
	payload["baseURL"] = "https://corp-a.example/v1"
	payload["baseURLs"] = []string{"https://corp-a.example/v1", "https://corp-b.example/v1"}
	payload["models"] = []string{"qwen-fast", "qwen-deep"}
	code, body := e.do(e.admin, "POST", "/api/workspaces", payload)
	if code != http.StatusAccepted {
		t.Fatalf("create multi-url: %d %s", code, body)
	}
	if !strings.Contains(body, `"baseURLs":["https://corp-a.example/v1","https://corp-b.example/v1"]`) {
		t.Fatalf("workspace response lacks baseURLs: %s", body)
	}
	wsID := wsIDof(t, body)
	ws := e.waitStatus(wsID, "running")
	if len(ws.BaseURLs) != 2 || ws.BaseURL != ws.BaseURLs[0] || ws.BaseURLs[1] != "https://corp-b.example/v1" {
		t.Fatalf("workspace base URLs = baseURL:%q baseURLs:%v", ws.BaseURL, ws.BaseURLs)
	}
	provider := e.rt.providers[wsID]
	if len(provider.AllBaseURLs()) != 2 || len(provider.AllModels()) != 2 {
		t.Fatalf("container provider did not receive lists (URLs=%d models=%d)", len(provider.AllBaseURLs()), len(provider.AllModels()))
	}
	paths := provision.Paths(e.home, "anna", wsID, ws.Name)
	settings, err := os.ReadFile(filepath.Join(paths.DshHome, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(settings)
	for _, want := range []string{"agctl-2:", "https://corp-a.example/v1", "https://corp-b.example/v1", "qwen-fast", "qwen-deep"} {
		if !strings.Contains(text, want) {
			t.Fatalf("multi-url settings.yaml lacks %q:\n%s", want, text)
		}
	}
	if got := strings.Count(text, "id: \"qwen-fast\""); got != 2 {
		t.Fatalf("shared model should be available under both URL providers, found %d aliases", got)
	}

	bad := validCreate("unsafe-url")
	bad["baseURL"] = "https://user:password@corp.example/v1"
	bad["baseURLs"] = []string{"https://user:password@corp.example/v1"}
	if code, response := e.do(e.admin, "POST", "/api/workspaces", bad); code != http.StatusBadRequest {
		t.Fatalf("URL credentials must be rejected, got %d %s", code, response)
	}
}
