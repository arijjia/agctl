package server

import (
	"strings"
	"testing"
)

// /map — за сессией: аноним 401, авторизованный видит archify-диаграмму.
func TestMapPageAuthAndServing(t *testing.T) {
	e := newTestEnv(t, 3)
	if code, _ := e.do(e.newClient(), "GET", "/map", nil); code != 401 {
		t.Fatalf("anonymous /map = %d, want 401", code)
	}
	if code, _ := e.do(e.newClient(), "GET", "/map/agctl-processes.html", nil); code != 401 {
		t.Fatalf("anonymous file = %d, want 401", code)
	}
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "GET", "/map", nil)
	if code != 200 || !contains(body, "Карта системы agctl") {
		t.Fatalf("map index = %d %q", code, firstLine(body))
	}
	if !contains(body, "/map/agctl-processes.html") || !contains(body, "/map?m=agctl-local") || !contains(body, "/map?m=agctl-cluster") || !contains(body, "/map?m=agctl-code") {
		t.Fatalf("index must show server map and tabs, got %q", body)
	}
	if _, body = e.do(e.admin, "GET", "/map?m=agctl-local", nil); !contains(body, `src="/map/agctl-local.html"`) {
		t.Fatalf("local tab must frame agctl-local.html, got %q", body)
	}
	code, body = e.do(e.admin, "GET", "/map/agctl-processes.html", nil)
	if code != 200 || !contains(body, "archify") {
		t.Fatalf("agctl-processes.html = %d, want 200 archify page", code)
	}
	// Имя вне белого списка — 404 даже авторизованным.
	if code, _ := e.do(e.admin, "GET", "/map/..%2Fsecrets.html", nil); code != 404 {
		t.Fatalf("traversal = %d, want 404", code)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && strings.Contains(s, sub) }

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
