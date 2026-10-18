package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCorsMiddleware(t *testing.T) {
	s := &Server{cfg: Config{CORSOrigins: []string{"http://127.0.0.1:3555", "https://cabinet.example.org/"}}}
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("разрешённый origin получает заголовки", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Origin", "http://127.0.0.1:3555")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:3555" {
			t.Fatalf("ACAO = %q, хотим origin", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("credentials = %q, хотим true", got)
		}
	})

	t.Run("чужой origin не получает заголовков", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Origin", "http://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, хотим пусто", got)
		}
	})

	t.Run("preflight замыкается на 204", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/workspaces", nil)
		req.Header.Set("Origin", "https://cabinet.example.org")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("код = %d, хотим 204", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://cabinet.example.org" {
			t.Fatalf("ACAO = %q (нужна нормализация trailing slash)", got)
		}
	})

	t.Run("без Origin запрос проходит без заголовков", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("код = %d, хотим 200", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("ACAO = %q, хотим пусто", got)
		}
	})
}

func TestCorsDisabledByDefault(t *testing.T) {
	s := &Server{cfg: Config{}}
	h := s.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3555")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("пустой CORSOrigins не должен выдавать ACAO, got %q", got)
	}
}
