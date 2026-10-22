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
	if code, _ := e.do(e.newClient(), "GET", "/map/overview.html", nil); code != 401 {
		t.Fatalf("anonymous file = %d, want 401", code)
	}
	e.register(e.admin, "anna", "secret1")
	code, body := e.do(e.admin, "GET", "/map", nil)
	if code != 200 || !contains(body, "Карта системы agctl") {
		t.Fatalf("map index = %d %q", code, firstLine(body))
	}
	if !contains(body, "/map/overview.html") {
		t.Fatalf("index must link overview.html, got %q", firstLine(body))
	}
	code, body = e.do(e.admin, "GET", "/map/overview.html", nil)
	if code != 200 || !contains(body, "archify") {
		t.Fatalf("overview.html = %d, want 200 archify page", code)
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
