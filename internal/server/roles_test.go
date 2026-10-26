package server

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// ── роль developer: заявка → апрув → привязка к учётке (v3 unified runtime) ──

// Полный поток: пользователь подаёт заявку, админ апрувит — роль
// привязывается к учётке и открывает developer-функции, но не legacy seats.
func TestRoleRequestFlow(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	env2 := e.newClient()
	e.register(env2, "bob", "secret2")

	// обычный пользователь не может создать dev-место — нужна роль
	code, body := e.do(env2, "POST", "/api/workspaces", map[string]any{
		"name": "bob-seat", "baseURL": "https://llm.example/v1", "model": "m1", "apiKey": "sk-test",
	})
	if code != http.StatusGone {
		t.Fatalf("legacy create = %d (%s), want 410", code, body)
	}

	// заявка на роль
	code, body = e.do(env2, "POST", "/api/role-requests", map[string]string{"motivation": "нужен терминал"})
	if code != http.StatusOK {
		t.Fatalf("role-request = %d %s", code, body)
	}
	// повторная заявка — конфликт
	if code, _ = e.do(env2, "POST", "/api/role-requests", nil); code != http.StatusConflict {
		t.Fatalf("повторная заявка = %d, want 409", code)
	}

	// админ видит заявку в очереди
	code, body = e.do(e.admin, "GET", "/api/role-requests", nil)
	if code != http.StatusOK || !contains(body, `"status":"pending"`) {
		t.Fatalf("admin list = %d %s", code, body)
	}

	// апрув привязывает роль к учётке
	code, body = e.do(e.admin, "POST", "/api/role-requests/1/approve", nil)
	if code != http.StatusOK {
		t.Fatalf("approve = %d %s", code, body)
	}
	code, body = e.do(env2, "GET", "/api/me", nil)
	if code != http.StatusOK || !contains(body, `"role":"developer"`) {
		t.Fatalf("после апрува роль не developer: %d %s", code, body)
	}

	// Developer расширяет возможности того же контейнера, но не выдаёт
	// разрешения создавать дополнительные legacy-контейнеры.
	if code, _ = e.do(env2, "POST", "/api/workspaces", nil); code != http.StatusGone {
		t.Fatalf("developer legacy create = %d, want 410", code)
	}

	// повторное решение по закрытой заявке — конфликт
	if code, _ = e.do(e.admin, "POST", "/api/role-requests/1/approve", nil); code != http.StatusConflict {
		t.Fatalf("повторный апрув = %d, want 409", code)
	}
}

// ── SSH-канал разработчика в персональном контейнере ─────────────────────────

func TestDeveloperSSHInChatContainer(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	const pub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAfDB4Nvu/3y+9tkkL4Z76eLpfEL65KBaWrWtApCmP21 developer@laptop"

	code, body := e.do(e.admin, "PUT", "/api/me/ssh", map[string]string{"pubKey": pub})
	if code != http.StatusOK {
		t.Fatalf("put ssh = %d %s", code, body)
	}
	code, body = e.do(e.admin, "GET", "/api/me/ssh", nil)
	if code != http.StatusOK || !contains(body, "ssh-ed25519") {
		t.Fatalf("get ssh = %d %s", code, body)
	}

	// открытие /chat/ (с сессией) → ensureChat получает SSHPubKey в спеке
	resp, err := e.admin.Get(e.api.URL + "/chat/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	rt, err := e.srv.ensureChat(1)
	if err != nil {
		t.Fatalf("ensureChat: %v", err)
	}
	if rt.handle.SSHPort == 0 {
		t.Fatalf("SSHPort не прокинут в handle (роль developer): %+v", rt.handle)
	}
	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		e.srv.chatMu.Lock()
		for _, spec := range e.rt.chatSpecs {
			got = spec.SSHPubKey
		}
		e.srv.chatMu.Unlock()
		if got != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got != pub {
		t.Fatalf("SSHPubKey не доехал до спецификации рантайма: %q", got)
	}

	// Пустой ключ снимает SSH при следующем старте; текущая сессия не ломается.
	code, body = e.do(e.admin, "PUT", "/api/me/ssh", map[string]string{"pubKey": ""})
	if code != http.StatusOK || !contains(body, "следующем старте") {
		t.Fatalf("clear ssh key = %d %s", code, body)
	}
	code, body = e.do(e.admin, "GET", "/api/me/ssh", nil)
	if code != http.StatusOK || !contains(body, `"pubKey":""`) {
		t.Fatalf("get cleared ssh key = %d %s", code, body)
	}
	// Simulate service-owned sleep (user-facing start/stop API is not exposed).
	e.srv.chatMu.Lock()
	current := e.srv.chats[1]
	delete(e.srv.chats, 1)
	e.srv.chatMu.Unlock()
	if current == nil {
		t.Fatal("expected running chat before simulated sleep")
	}
	if err := e.rt.StopChat(context.Background(), current.handle); err != nil {
		t.Fatalf("service stop: %v", err)
	}
	next, err := e.srv.ensureChat(1)
	if err != nil {
		t.Fatal(err)
	}
	if next.handle.SSHPort != 0 {
		t.Fatalf("cleared key still enabled sshd on next boot: %+v", next.handle)
	}
}

// Обычный пользователь (роль user) SSH-канал не получает вовсе.
func TestSSHForbiddenForUserRole(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	other := e.newClient()
	e.register(other, "bob", "secret2")
	code, _ := e.do(other, "PUT", "/api/me/ssh", map[string]string{"pubKey": "ssh-ed25519 AAA"})
	if code != http.StatusForbidden {
		t.Fatalf("put ssh без роли = %d, want 403", code)
	}
}

// TTL персонального контейнера по умолчанию — 1 час (решение владельца v3).
func TestChatTTLDefaultIsOneHour(t *testing.T) {
	e := newTestEnv(t, 3)
	if e.srv.cfg.ChatTTL != time.Hour {
		t.Fatalf("ChatTTL = %v, want 1h", e.srv.cfg.ChatTTL)
	}
}
