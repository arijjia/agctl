package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Адаптеры с общим Endpoint (AX/Substrate: маршрутизатор atenet-router)
// выбирают актор заголовком handle.Headers — шлюз обязан добавлять его к
// каждому проксируемому запросу.
func TestChatProxyAddsRuntimeHandleHeaders(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.Tier0 = true })
	e.register(e.admin, "anna", "secret1")
	user := e.newClient()
	e.register(user, "bob", "secret2")

	var seen http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		fmt.Fprint(w, "actor-alive")
	}))
	defer upstream.Close()
	e.rt.chatEndpointOverride = upstream.URL
	e.rt.chatHeaders = map[string]string{"ate-target-actor": "agctl/agctl-chat-bob"}

	code, body := e.getChatHTML(user)
	if code != http.StatusOK || !strings.Contains(body, "actor-alive") {
		t.Fatalf("proxy: code=%d body=%q", code, body)
	}
	if got := seen.Get("ate-target-actor"); got != "agctl/agctl-chat-bob" {
		t.Fatalf("upstream did not receive the actor header: %q (headers %v)", got, seen)
	}
}
