package server

import (
	"net/http"
	"testing"
)

// Старые URL остаются явными tombstone-ручками, а не скрытым обходом
// единого рантайма. Даже администратор не может создать второй контейнер.
func TestLegacyWorkspacesRetired(t *testing.T) {
	e := newTestEnv(t, 10)
	e.register(e.admin, "admin", "secret123")
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/workspaces"}, {"POST", "/api/workspace-requests"}, {"GET", "/api/workspace-requests"},
		{"POST", "/api/workspace-requests/old/approve"}, {"POST", "/api/workspace-requests/old/reject"},
		{"POST", "/api/workspaces/old/start"}, {"POST", "/api/workspaces/old/stop"},
		{"DELETE", "/api/workspaces/old"}, {"PATCH", "/api/workspaces/old/provider"}, {"PATCH", "/api/workspaces/old/ssh"},
	} {
		if c, b := e.do(e.admin, tc.method, tc.path, map[string]any{}); c != http.StatusGone {
			t.Errorf("%s %s = %d %s, want 410", tc.method, tc.path, c, b)
		}
	}
	if len(e.rt.started) != 0 {
		t.Fatal("retired API started a runtime")
	}
	if c, _ := e.do(e.newClient(), "POST", "/api/workspaces", nil); c != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d, want 401", c)
	}
}
