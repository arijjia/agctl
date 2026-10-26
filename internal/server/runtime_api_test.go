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

	// Requests wake the runtime; the status endpoint remains read-only.
	if _, err := e.srv.ensureChat(1); err != nil {
		t.Fatal(err)
	}
	if code, body := e.do(e.admin, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"running"`) {
		t.Fatalf("status after agctl wake = %d %s", code, body)
	}
	if code, body := e.do(user, "GET", "/api/me/runtime", nil); code != http.StatusOK || !strings.Contains(body, `"status":"sleeping"`) {
		t.Fatalf("other user's status = %d %s", code, body)
	}

	// No user-facing lifecycle/log endpoints: agctl serve and TTL reaper own it.
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/me/runtime/start"},
		{"POST", "/api/me/runtime/stop"},
		{"GET", "/api/me/runtime/logs"},
	} {
		if code, body := e.do(e.admin, tc.method, tc.path, nil); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d %s, want a non-routable status", tc.method, tc.path, code, body)
		}
	}
	if got := e.rt.chatStops[e.srv.chatRuntimeID(1)]; got != 0 {
		t.Fatalf("runtime control request stopped provider %d times", got)
	}
}
