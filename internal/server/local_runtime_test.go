package server

import (
	"net/http"
	"strings"
	"testing"
)

// runtimeHooks — fakeHooks с выбором рантайма (LocalRuntimeHooks).
type runtimeHooks struct {
	*fakeHooks
	kind   string
	sbxErr string // непусто — sbx недоступен
}

func (h *runtimeHooks) RuntimeKind() string { return h.kind }
func (h *runtimeHooks) Runtimes() []LocalRuntime {
	return []LocalRuntime{
		{ID: "docker", Title: "Docker", Available: true},
		{ID: "sbx", Title: "Docker Sandboxes", Available: h.sbxErr == "", Detail: h.sbxErr},
	}
}
func (h *runtimeHooks) SetRuntimeKind(kind string) error { h.kind = kind; return nil }

func TestLocalRuntimeChoice(t *testing.T) {
	env, fh := newLocalUIEnv(t)
	c := env.newClient()
	if code, _ := env.do(c, "GET", "/local/login?token=login-tok", nil); code != http.StatusSeeOther {
		t.Fatalf("вход: %d", code)
	}
	// хост без выбора рантайма: только docker, смена недоступна
	_, body := env.do(c, "GET", "/api/local/ui", nil)
	if !strings.Contains(body, `"runtimeKind":"docker"`) || !strings.Contains(body, `"runtimes":[{"id":"docker"`) || strings.Contains(body, `"id":"sbx"`) {
		t.Fatalf("/api/local/ui без хуков рантайма: %s", body)
	}
	if code, _ := env.do(c, "PUT", "/api/local/ui/runtime", map[string]any{"runtime": "sbx"}); code != http.StatusNotFound {
		t.Fatalf("смена рантайма без хуков: %d", code)
	}

	h := &runtimeHooks{fakeHooks: fh, kind: "docker", sbxErr: "sbx не установлен"}
	env.srv.cfg.Local.Hooks = h
	_, body = env.do(c, "GET", "/api/local/ui", nil)
	if !strings.Contains(body, `"id":"sbx"`) || !strings.Contains(body, `"available":false`) || !strings.Contains(body, "sbx не установлен") {
		t.Fatalf("/api/local/ui: недоступный sbx не показан: %s", body)
	}
	if code, body := env.do(c, "PUT", "/api/local/ui/runtime", map[string]any{"runtime": "sbx"}); code != http.StatusConflict || !strings.Contains(body, "sbx не установлен") {
		t.Fatalf("недоступный рантайм должен отклоняться с причиной: %d %s", code, body)
	}
	if code, _ := env.do(c, "PUT", "/api/local/ui/runtime", map[string]any{"runtime": "lxc"}); code != http.StatusBadRequest {
		t.Fatalf("неизвестный рантайм: %d", code)
	}
	if h.kind != "docker" {
		t.Fatalf("отклонённая смена изменила рантайм: %s", h.kind)
	}

	h.sbxErr = ""
	code, body := env.do(c, "PUT", "/api/local/ui/runtime", map[string]any{"runtime": "sbx"})
	if code != 200 || !strings.Contains(body, `"runtime":"sbx"`) || !strings.Contains(body, `"restartNeeded":false`) {
		t.Fatalf("смена рантайма: %d %s", code, body)
	}
	if _, body = env.do(c, "GET", "/api/local/ui", nil); !strings.Contains(body, `"runtimeKind":"sbx"`) {
		t.Fatalf("выбранный рантайм не виден: %s", body)
	}
}
