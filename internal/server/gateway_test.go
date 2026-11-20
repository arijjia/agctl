package server

// Тесты прокси чата /chat/ (chat.go + транспорт gateway.go): mint-цепочка
// 401 → 303+Set-Cookie → 200, SSE/WebSocket, combo-query и изоляция.
// Upstream — httptest сервер с той же схемой авторизации, что у dsh web 0.2.0+.

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
	"strings"
	"sync"
	"testing"
	"time"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// newFakeDsh — upstream, повторяющий контракт dsh web: без cookie — 401;
// ?token=<tok> — 303 + Set-Cookie dsh-auth-test=ok (Path=/) + Location "/";
// с валидной cookie и Origin, совпадающим с Host, — 200; несовпадающий Origin — 403.
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
		if origin := r.Header.Get("Origin"); origin != "" {
			parsed, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(parsed.Host, r.Host) {
				w.WriteHeader(http.StatusForbidden)
				return
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

// plantChat — живой чат-рантайм пользователя uid (роль user, слот 0),
// указывающий на фейковый upstream: фикстура не вызывает provision,
// boot-токен dsh появляется в «логах контейнера» как у реального рантайма.
func plantChat(t *testing.T, e *testEnv, uid int64, upstreamURL, token string) {
	t.Helper()
	handle := agctlrt.ChatHandle{
		RuntimeID:  e.srv.chatRuntimeID(uid, 0),
		InstanceID: "gw-" + token,
		Endpoint:   upstreamURL,
	}
	e.rt.logs[handle.InstanceID] = "dsh web: http://0.0.0.0:3080/?token=" + token
	e.srv.forgetToken(chatTokenCacheKey(handle))
	e.srv.chatMu.Lock()
	e.srv.chats[chatKey{uid, 0}] = &chatRuntime{handle: handle, lastSeen: time.Now()}
	e.srv.chatMu.Unlock()
}

// ownerClient — непривилегированный пользователь чата (admin в v3 получает
// 307 в /admin, личный чат ему не выдаётся).
func ownerClient(t *testing.T, e *testEnv) (*http.Client, int64) {
	t.Helper()
	c := e.newClient()
	e.register(c, "chat-owner", "secret9")
	return c, 2 // второй зарегистрированный: uid=1 — admin
}

// TestGatewayMintChain — первый вход без dsh-cookie: upstream 401 → прокси
// серверно mint-ит по boot-токену → браузеру уходит 303 + Set-Cookie +
// Location /w/{id}/; повторный запрос с cookie — 200 от upstream.
func TestGatewayMintChain(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-1")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-tok-1")

	// 1) вход без cookie: ждём 303 с переписанным Location и cookie
	req, _ := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("первый вход: %d, ждали 303 (mint)", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/chat/" {
		t.Fatalf("Location %q, ждали /chat/", loc)
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
	if want := "/chat/"; dshCookie.Path != want {
		t.Fatalf("dsh cookie Path=%q, ждали ограничение workspace %q", dshCookie.Path, want)
	}

	// 2) повторный запрос: cookie из jar (testJar хранит по хосту) → 200 upstream
	req, _ = http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp2, err := owner.Do(req)
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

// TestGatewayIsolation — аноним не проходит в /chat/; чужой пользователь
// получает свой чат, а не чужой upstream.
func TestGatewayIsolation(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-2")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-tok-2")

	req, _ := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("аноним: %d, ждали 401", resp.StatusCode)
	}

	// другой пользователь: свой пустой рантайм, чужой upstream недоступен
	other := e.newClient()
	e.register(other, "bob", "secret2")
	resp2, err := other.Get(e.api.URL + "/chat/")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode == http.StatusOK {
		t.Fatalf("чужой пользователь получил чужой upstream: %d", resp2.StatusCode)
	}
	_ = owner
}

// TestGatewayPathRewrite — префикс /w/{id}/ срезается, upstream видит /.
func TestGatewayPathRewrite(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-tok-3")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-tok-3")

	// mint-вход, затем запрос под-пути с cookie напрямую через upstream-клиент
	req, _ := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d", resp.StatusCode)
	}
	// повторный запрос с jar-ом cookie: клиент шлёт dsh-auth-* → upstream 200
	req, _ = http.NewRequest("GET", e.api.URL+"/chat/some/deep/path", nil)
	resp2, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	body := readAll(t, resp2)
	if !strings.Contains(body, "upstream:/some/deep/path") {
		t.Fatalf("upstream видит %q, ждали /some/deep/path", body)
	}
}

// TestGatewayComboQuery — combo-URL плагинов dsh (`/plugins/??модули&rev=`)
// доходят до upstream с query побайтово: dsh парсит сырую строку (ведущий
// литеральный '?'), любое перекодирование/пересортировка даёт upstream-404,
// и браузер падает «HTML did not preload @deepseek-ai/dsh-client-modules/client.js».
// Заодно проверяем, что ?token= от браузера по-прежнему вырезается, а прочие
// параметры остаются нетронутыми.
func TestGatewayComboQuery(t *testing.T) {
	const combo = `?@deepseek-ai/dsh-client-modules/client.js&rev=efd1797d4790`
	var mu sync.Mutex
	seen := map[string]string{} // path → последний RawQuery
	mux := http.NewServeMux()
	authed := func(r *http.Request) bool {
		for _, c := range r.Cookies() {
			if c.Name == "dsh-auth-test" && c.Value == "ok" {
				return true
			}
		}
		return false
	}
	mux.HandleFunc("/plugins/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen["/plugins/"] = r.URL.RawQuery
		mu.Unlock()
		if !authed(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// dsh принимает combo только с точной сырой строкой query
		if r.URL.RawQuery == combo {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "boot-combo" {
			http.SetCookie(w, &http.Cookie{Name: "dsh-auth-test", Value: "ok", Path: "/"})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if authed(r) {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	upstream := httptest.NewServer(mux)
	t.Cleanup(upstream.Close)

	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-combo")

	resp, err := owner.Get(e.api.URL + "/chat/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d, ждали 303", resp.StatusCode)
	}

	resp2, err := owner.Get(e.api.URL + "/chat/plugins/??@deepseek-ai/dsh-client-modules/client.js&rev=efd1797d4790")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	mu.Lock()
	got := seen["/plugins/"]
	mu.Unlock()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("combo-query: upstream %d, ждали 200; upstream увидел RawQuery %q, ждали %q", resp2.StatusCode, got, combo)
	}

	// ?token= от браузера вырезается, остальное — без изменений
	resp3, err := owner.Get(e.api.URL + "/chat/plugins/??x/y&rev=1&token=evil")
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	mu.Lock()
	got = seen["/plugins/"]
	mu.Unlock()
	if got != `?x/y&rev=1` {
		t.Fatalf("token не вырезан побайтово: upstream увидел %q", got)
	}
}

// TestGatewayProxyMethods — DSH использует POST для RPC (например,
// /api/directoryPicker/list). Gateway должен проксировать не только GET:
// иначе Go ServeMux отвечает 405 до ReverseProxy, и dsh-клиент не может
// перечислить каталоги.
func TestGatewayProxyMethods(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-methods")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-methods")

	// Первый GET минтит DSH-cookie для workspace-префикса.
	resp, err := owner.Get(e.api.URL + "/chat/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d, ждали 303", resp.StatusCode)
	}

	// Typed RPC dsh-client-connection отправляет POST на этот endpoint.
	req, err := http.NewRequest(http.MethodPost, e.api.URL+"/chat/api/directoryPicker/list", strings.NewReader(`{"type":"client-request"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	apiOrigin, err := url.Parse(e.api.URL)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", apiOrigin.Scheme+"://"+apiOrigin.Host)
	resp, err = owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "upstream:/api/directoryPicker/list") {
		t.Fatalf("POST directoryPicker/list: %d %q, запрос должен дойти до upstream с Origin, совпадающим с его Host", resp.StatusCode, body)
	}

	// Переписывание внутреннего Origin не должно превращать чужой Origin в
	// same-origin: gateway обязан отклонить его до upstream.
	req, err = http.NewRequest(http.MethodPost, e.api.URL+"/chat/api/directoryPicker/list", strings.NewReader(`{"type":"client-request"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example")
	resp, err = owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d, ждали 403", resp.StatusCode)
	}

	// При этом API не становится публичным: без agctl-сессии — 401.
	anonReq, err := http.NewRequest(http.MethodPost, e.api.URL+"/chat/api/directoryPicker/list", strings.NewReader(`{"type":"client-request"}`))
	if err != nil {
		t.Fatal(err)
	}
	anonReq.Header.Set("Origin", apiOrigin.Scheme+"://"+apiOrigin.Host)
	anon, err := e.newClient().Do(anonReq)
	if err != nil {
		t.Fatal(err)
	}
	anon.Body.Close()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Fatalf("анонимный POST directoryPicker/list: %d, ждали 401", anon.StatusCode)
	}
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// TestGatewaySecureCookie — TLS-режим помечает DSH-cookie Secure до браузера.
func TestGatewaySecureCookie(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-secure")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-secure")
	e.srv.cfg.SecureCookies = true

	resp, err := owner.Get(e.api.URL + "/chat/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("mint: %d", resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, "dsh-auth-") {
			if !c.Secure || c.Path != "/chat/" {
				t.Fatalf("secure DSH cookie flags: %+v", c)
			}
			return
		}
	}
	t.Fatal("mint response has no dsh-auth cookie")
}

// TestGatewayWarmupPage — браузер получает авто-ретраящую страницу вместо
// JSON-ошибки, если контейнер ещё греется (провайдер недоступен).
func TestGatewayWarmupPage(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, _ := ownerClient(t, e)
	e.srv.chatProvider = nil // рантайм не может подняться — «прогрев»

	req, _ := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("warmup: %d, ждали 503", resp.StatusCode)
	}
	if !strings.Contains(body, `http-equiv="refresh"`) || !strings.Contains(body, "/chat/") {
		t.Fatalf("warmup page without meta refresh: %q", body)
	}

	// не-браузерный клиент получает JSON-ошибку как раньше
	req, _ = http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp2, err := owner.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	jsonBody := readAll(t, resp2)
	if resp2.StatusCode != http.StatusServiceUnavailable || !strings.Contains(jsonBody, "прогревается") {
		t.Fatalf("api client: %d %q", resp2.StatusCode, jsonBody)
	}
}

// TestGatewaySSE — поток событий проходит через gateway без потери чанков.
func TestGatewaySSE(t *testing.T) {
	upstream, _ := newFakeDsh(t, "boot-sse")
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-sse")
	for i := 0; i < 2; i++ {
		resp, err := owner.Get(e.api.URL + "/chat/")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	resp, err := owner.Get(e.api.URL + "/chat/events")
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
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "boot-ws")

	for i := 0; i < 2; i++ {
		resp, err := owner.Get(e.api.URL + "/chat/")
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
	for _, c := range owner.Jar.Cookies(apiURL) {
		cookies = append(cookies, c.Name+"="+c.Value)
	}
	cookieHeader := strings.Join(cookies, "; ")
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	_, _ = fmt.Fprintf(conn, "GET /chat/socket HTTP/1.1\r\nHost: %s\r\nOrigin: http://%s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nCookie: %s\r\n\r\n", apiURL.Host, apiURL.Host, key, cookieHeader)
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
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	owner, ownerUID := ownerClient(t, e)
	plantChat(t, e, ownerUID, upstream.URL, "unreachable")
	e.rt.logs["gw-unreachable"] = "" // токена нет

	req, _ := http.NewRequest("GET", e.api.URL+"/chat/", nil)
	resp, err := owner.Do(req)
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
