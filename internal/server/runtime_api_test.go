package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestMyRuntimeStatusReadOnly(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	user := e.newClient()
	e.register(user, "bob", "secret2")

	// Any authenticated user can read only their own runtime status.
	if code, body := e.do(user, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("user initial status = %d %s", code, body)
	}
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("admin initial status = %d %s", code, body)
	}

	// Запрос статуса будит рантайм? Нет — статус read-only: ensureChat зовёт
	// только платформа (лаунчер-старт / chatProxy / админ), не сам GET.
	if _, err := e.srv.ensureChat(2); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(user, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("status after agctl wake = %d %s", code, body)
	}
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("admin status = %d %s, ждём sleeping (админ без рантайма)", code, body)
	}

	// Лifecycle-эндпоинты лаунчера: developer стартует/останавливает себя,
	// логи своего контейнера читает через /api/me/runtime/logs (админ и
	// обычный user lifecycle не получают).
	dev := makeDeveloper(e, "dev1", "devpass1")
	if code, body := e.do(dev, "POST", "/api/me/runtime/start", nil); code != http.StatusOK || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("developer start = %d %s", code, body)
	}
	if code, body := e.do(dev, "POST", "/api/me/runtime/stop", nil); code != http.StatusOK || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("developer stop = %d %s", code, body)
	}
	if code, _ := e.do(dev, "GET", "/api/me/runtime/logs", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("logs = %d, ждём 503 (fake-рантайм без каталога контейнеров; с каталогом — TestLauncherRuntimeLogs)", code)
	}
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/me/runtime/start"},
		{"POST", "/api/me/runtime/stop"},
	} {
		for _, c := range []*http.Client{e.admin, user} {
			if code, body := e.do(c, tc.method, tc.path, nil); code != http.StatusForbidden {
				t.Fatalf("%s %s = %d %s, ждём 403 (только developer)", tc.method, tc.path, code, body)
			}
		}
	}
	if got := e.rt.chatStops[e.srv.chatRuntimeID(1)]; got != 0 {
		t.Fatalf("runtime control request stopped provider %d times", got)
	}
}
