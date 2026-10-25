package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestMyRuntimeDeveloperControls — developer-контролы личного рантайма:
// статус/старт/стоп/логи; роль user получает 403.
func TestMyRuntimeDeveloperControls(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	env2 := e.newClient()
	e.register(env2, "bob", "secret2") // роль user

	// user — 403 на все эндпоинты.
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/me/runtime"},
		{"POST", "/api/me/runtime/start"},
		{"POST", "/api/me/runtime/stop"},
		{"GET", "/api/me/runtime/logs"},
	} {
		if code, _ := e.do(env2, tc.method, tc.path, nil); code != http.StatusForbidden {
			t.Fatalf("%s %s как user = %d, want 403", tc.method, tc.path, code)
		}
	}

	// Спящий рантайм: статус sleeping, логи — 409.
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != 200 || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("initial status = %d %s", code, body)
	}
	if code, _ := e.do(e.admin, "GET", "/api/me/runtime/logs", nil); code != http.StatusConflict {
		t.Fatalf("sleeping logs = %d, want 409", code)
	}

	// Старт поднимает runtime; статус running; логи читаются.
	if code, body := e.do(e.admin, "POST", "/api/me/runtime/start", nil); code != 200 || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("start = %d %s", code, body)
	}
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != 200 || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("status after start = %d %s", code, body)
	}
	code, body := e.do(e.admin, "GET", "/api/me/runtime/logs?tail=50", nil)
	if code != 200 || !strings.Contains(body, "dsh web listening") {
		t.Fatalf("logs = %d %q", code, body)
	}

	// Стоп удаляет compute, но не том: статус снова sleeping, повторный стоп — no-op.
	if code, body := e.do(e.admin, "POST", "/api/me/runtime/stop", nil); code != 200 || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("stop = %d %s", code, body)
	}
	if got := e.rt.chatStops[e.srv.chatRuntimeID(1)]; got != 1 {
		t.Fatalf("StopChat calls = %d, want 1", got)
	}
	if code, _ := e.do(e.admin, "POST", "/api/me/runtime/stop", nil); code != 200 {
		t.Fatalf("idle stop = %d, want 200", code)
	}
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != 200 || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("status after stop = %d %s", code, body)
	}
}
