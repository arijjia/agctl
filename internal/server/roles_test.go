package server

import (
	"net/http"
	"testing"
	"time"
)

// ── роль developer: заявка → апрув → привязка к учётке (v3 unified runtime) ──

// Полный поток: обычный пользователь подаёт заявку (dev-места ему закрыты),
// админ апрувит — роль привязывается к учётке и открывает создание мест.
func TestRoleRequestFlow(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	env2 := e.newClient()
	e.register(env2, "bob", "secret2")

	// обычный пользователь не может создать dev-место — нужна роль
	code, body := e.do(env2, "POST", "/api/workspaces", map[string]any{
		"name": "bob-seat", "baseURL": "https://llm.example/v1", "model": "m1", "apiKey": "sk-test",
	})
	if code != http.StatusForbidden {
		t.Fatalf("create без роли = %d (%s), want 403", code, body)
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

	// роль открывает создание dev-мест (provision асинхронный — ждём running,
	// чтобы TempDir-чистка не убегала вперёд горутины)
	code, _ = e.do(env2, "POST", "/api/workspaces", map[string]any{
		"name": "bob-seat", "baseURL": "https://llm.example/v1", "model": "m1", "apiKey": "sk-test",
	})
	if code != http.StatusAccepted {
		t.Fatalf("create с ролью = %d, want 202", code)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		wss, err := e.db.WorkspacesAll()
		if err == nil {
			for _, w := range wss {
				if w.Name == "bob-seat" && w.Status == "running" {
					goto provisioned
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
provisioned:

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
