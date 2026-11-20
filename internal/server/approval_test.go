package server

import (
	"net/http"
	"testing"
)

// v2-эндпоинты воркспейсов удалены целиком (карточка
// legacy-workspace-code-cleanup): маршрутов больше нет — ни ответа, ни
// скрытого обхода единого рантайма. ServeMux отдаёт 404/405.
func TestLegacyWorkspacesRemoved(t *testing.T) {
	e := newTestEnv(t, 10)
	e.register(e.admin, "admin", "secret123")
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/workspaces"}, {"POST", "/api/workspace-requests"}, {"GET", "/api/workspace-requests"},
		{"POST", "/api/workspace-requests/old/approve"}, {"POST", "/api/workspace-requests/old/reject"},
		{"POST", "/api/workspaces/old/start"}, {"POST", "/api/workspaces/old/stop"},
		{"DELETE", "/api/workspaces/old"}, {"PATCH", "/api/workspaces/old/provider"}, {"PATCH", "/api/workspaces/old/ssh"},
		{"GET", "/api/templates"}, {"GET", "/api/kits"}, {"GET", "/w/old/"}, {"POST", "/w/old/"},
	} {
		if c, _ := e.do(e.admin, tc.method, tc.path, map[string]any{}); c != http.StatusNotFound && c != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 404/405 (маршрут удалён)", tc.method, tc.path, c)
		}
	}
	if len(e.rt.started) != 0 {
		t.Fatal("retired API started a runtime")
	}
}
