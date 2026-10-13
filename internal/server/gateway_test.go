package server

// Тесты шлюза /w/{id}/ (gateway.go): проверка сессии, mint-цепочка
// 401 → 303+Set-Cookie → 200 и изоляция пользователей. Upstream — httptest
// сервер с той же схемой авторизации, что у dsh web 0.2.0+.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// newFakeDsh — upstream, повторяющий контракт dsh web: без cookie — 401;
// ?token=<tok> — 303 + Set-Cookie dsh-auth-test=ok (Path=/) + Location "/";
// с валидной cookie — 200 и тело "upstream:<path>".
func newFakeDsh(t *testing.T, token string) (*httptest.Server, *http.ServeMux) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == token {
			if r.URL.Path != "/" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name: "dsh-auth-test", Value: "ok", Path: "/",
				HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		validCookie := false
		for _, c := range r.Cookies() {
			if c.Name == "dsh-auth-test" && c.Value == "ok" {
				validCookie = true
			}
		}
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			if !validCookie {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("upstream writer does not support hijacking")
				return
			}
			conn, rw, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			accept := websocketAccept(r.Header.Get("Sec-WebSocket-Key"))
			_, _ = fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
			_ = rw.Flush()
			_ = conn.Close()
			return
		}
		if validCookie {
			if r.URL.Path == "/events" {
				w.Header().Set("Content-Type", "text/event-stream")
				flusher, ok := w.(http.Flusher)
				if !ok {
					t.Error("upstream writer does not support flushing")
					return
				}
				_, _ = fmt.Fprint(w, "data: first\n\n")
				flusher.Flush()
				_, _ = fmt.Fprint(w, "data: second\n\n")
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("upstream:" + r.URL.Path))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("dsh web authentication required"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, mux
}

// pointUpstream — переключает running-воркспейс на порт фейкового upstream.
func pointUpstream(t *testing.T, e *testEnv, wsID, upstreamURL string) {
	t.Helper()
	u, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	ws, err := e.db.WorkspaceByID(wsID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.UpdateWorkspaceRuntime(wsID, ws.ContainerID, port, "running"); err != nil {
		t.Fatal(err)
	}
}

// runningWorkspace — создаёт воркспейс и ждёт running.
func runningWorkspace(t *testing.T, e *testEnv, name string) string {
	t.Helper()
	code, body := e.do(e.admin, "POST", "/api/workspaces", validCreate(name))
	if code != http.StatusAccepted {
		t.Fatalf("create %s: %d %s", name, code, body)
	}
	wsID := wsIDof(t, body)
	e.waitStatus(wsID, "running")
	return wsID
}

// TestGatewayMintChain — первый вход без dsh-cookie: upstream 401 → прокси
// серверно mint-ит по boot-токену → браузеру уходит 303 + Set-Cookie +
// Location /w/{id}/; повторный запрос с cookie — 200 от upstream.
func TestGatewayMintChain(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-1")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-mint")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=boot-tok-1"
	e.srv.forgetToken(wsID)

	// 1) вход без cookie: ждём 303 с переписанным Location и cookie
	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("первый вход: %d, ждали 303 (mint)", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/w/"+wsID+"/" {
		t.Fatalf("Location %q, ждали /w/%s/", loc, wsID)
	}
	var dshCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "dsh-auth-") {
			dshCookie = c
		}
	}
	if dshCookie == nil {
		t.Fatal("mint-ответ без Set-Cookie dsh-auth-*")
	}
	if want := "/w/" + wsID + "/"; dshCookie.Path != want {
		t.Fatalf("dsh cookie Path=%q, ждали ограничение workspace %q", dshCookie.Path, want)
	}

	// 2) повторный запрос: cookie из jar (testJar хранит по хосту) → 200 upstream
	req, _ = http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp2, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body := readAll(t, resp2)
	if resp2.StatusCode != http.StatusOK || !strings.Contains(body, "upstream:/") {
		t.Fatalf("повторный вход: %d %q", resp2.StatusCode, body)
	}

	// токен нигде не был отдан браузеру: в jar agctl-домена нет ?token=,
	// а тело ответа не содержит boot-токен
	if strings.Contains(body, "boot-tok-1") {
		t.Fatal("boot-токен протёк в тело ответа")
	}
}

// TestGatewayIsolation — чужой пользователь и аноним не проходят в воркспейс.
func TestGatewayIsolation(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-2")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	other := e.newClient()
	e.register(other, "bob", "secret2")
	wsID := runningWorkspace(t, e, "gw-iso")
	pointUpstream(t, e, wsID, upstream.URL)

	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err := other.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("чужой: %d, ждали 403", resp.StatusCode)
	}

	req, _ = http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err = (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("аноним: %d, ждали 401", resp.StatusCode)
	}
}

// TestGatewayPathRewrite — префикс /w/{id}/ срезается, upstream видит /.
func TestGatewayPathRewrite(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-3")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-path")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=boot-tok-3"
	e.srv.forgetToken(wsID)

	// mint-вход, затем запрос под-пути с cookie напрямую через upstream-клиент
	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d", resp.StatusCode)
	}
	// повторный запрос с jar-ом cookie: клиент шлёт dsh-auth-* → upstream 200
	req, _ = http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/some/deep/path", nil)
	resp2, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body := readAll(t, resp2)
	if !strings.Contains(body, "upstream:/some/deep/path") {
		t.Fatalf("upstream видит %q, ждали /some/deep/path", body)
	}
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// TestGatewaySecureCookie — TLS-режим помечает DSH-cookie Secure до браузера.
func TestGatewaySecureCookie(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-secure")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-secure")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=boot-secure"
	e.srv.forgetToken(wsID)
	e.srv.cfg.SecureCookies = true

	resp, err := e.admin.Get(e.api.URL + "/w/" + wsID + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "dsh-auth-") {
			if !c.Secure || c.Path != "/w/"+wsID+"/" {
				t.Fatalf("secure DSH cookie flags: %+v", c)
			}
			return
		}
	}
	t.Fatal("mint response has no dsh-auth cookie")
}

// TestGatewayWarmupPage — браузер получает авто-ретраящую страницу вместо
// JSON-ошибки, если контейнер ещё греется (upstream недоступен).
func TestGatewayWarmupPage(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-warm")
	// указываем порт, где никто не слушает — имитация прогрева
	if err := e.db.UpdateWorkspaceRuntime(wsID, "ctr-"+wsID, 1, "running"); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("warmup: %d, ждали 503", resp.StatusCode)
	}
	if !strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, "/w/"+wsID+"/") {
		t.Fatalf("warmup page without meta refresh: %q", body)
	}

	// не-браузерный клиент получает JSON-ошибку как раньше
	req, _ = http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp2, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	jsonBody := readAll(t, resp2)
	if resp2.StatusCode != http.StatusBadGateway || !strings.Contains(jsonBody, "недоступен") {
		t.Fatalf("api client: %d %q", resp2.StatusCode, jsonBody)
	}
}

// TestGatewaySSE — поток событий проходит через gateway без потери чанков.
func TestGatewaySSE(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-sse")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-sse")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=boot-sse"
	e.srv.forgetToken(wsID)
	for i := 0; i < 2; i++ {
		resp, err := e.admin.Get(e.api.URL + "/w/" + wsID + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	resp, err := e.admin.Get(e.api.URL + "/w/" + wsID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE response: status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !strings.Contains(body, "data: first\n\n") || !strings.Contains(body, "data: second\n\n") {
		t.Fatalf("SSE chunks lost: %q", body)
	}
}

// TestGatewayWebSocketUpgrade — проверяет реальный HTTP Upgrade через agctl
// ReverseProxy после dsh cookie-mint, а не только обычные GET-запросы.
func TestGatewayWebSocketUpgrade(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-ws")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-ws")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "dsh web: http://0.0.0.0:3080/?token=boot-ws"
	e.srv.forgetToken(wsID)

	for i := 0; i < 2; i++ {
		resp, err := e.admin.Get(e.api.URL + "/w/" + wsID + "/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := http.StatusSeeOther
		if i == 1 {
			want = http.StatusOK
		}
		if resp.StatusCode != want {
			t.Fatalf("warmup %d: %d, ждали %d", i, resp.StatusCode, want)
		}
	}

	apiURL, _ := url.Parse(e.api.URL)
	conn, err := net.Dial("tcp", apiURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var cookies []string
	for _, c := range e.admin.Jar.Cookies(apiURL) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	cookieHeader := strings.Join(cookies, "; ")
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	_, _ = fmt.Fprintf(conn, "GET /w/%s/socket HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nCookie: %s\r\n\r\n", wsID, apiURL.Host, key, cookieHeader)
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("websocket upgrade: %d, ждали 101", resp.StatusCode)
	}
	if got := resp.Header.Get("Sec-WebSocket-Accept"); got != websocketAccept(key) {
		t.Fatalf("Sec-WebSocket-Accept=%q", got)
	}
}

// TestRewriteLocation — таблица перенаправлений Location mint-ответа.
func TestRewriteLocation(t *testing.T) {
	cases := []struct{ in, prefix, upstream, want string }{
		{"/", "/w/w1", "127.0.0.1:5555", "/w/w1/"},
		{"/?x=1", "/w/w1", "127.0.0.1:5555", "/w/w1/?x=1"},
		{"http://127.0.0.1:5555/", "/w/w2", "127.0.0.1:5555", "/w/w2/"},
		{"http://127.0.0.1:5555/app?a=b", "/w/w2", "127.0.0.1:5555", "/w/w2/app?a=b"},
		{"/w/w3/", "/w/w3", "127.0.0.1:5555", "/w/w3/"},
		{"login", "/w/w4", "127.0.0.1:5555", "/w/w4/login"},
		{"https://example.org/login", "/w/w5", "127.0.0.1:5555", "https://example.org/login"},
	}
	for _, c := range cases {
		if got := rewriteLocation(c.in, c.prefix, c.upstream); got != c.want {
			t.Errorf("rewriteLocation(%q, %q, %q) = %q, ждали %q", c.in, c.prefix, c.upstream, got, c.want)
		}
	}
}

// TestGatewayNoToken — если boot-токен недоступен, upstream-401 проходит
// как есть (браузер получает честный отказ), а не зависает.
func TestGatewayNoToken(t *testing.T) {
	upstream, _ := newFakeDsh(t, "unreachable")
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	wsID := runningWorkspace(t, e, "gw-notok")
	pointUpstream(t, e, wsID, upstream.URL)
	e.rt.logs[wsID] = "" // токена нет
	e.srv.forgetToken(wsID)

	req, _ := http.NewRequest("GET", e.api.URL+"/w/"+wsID+"/", nil)
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("без токена: %d, ждали 401 насквозь", resp.StatusCode)
	}
	_ = context.Background()
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
