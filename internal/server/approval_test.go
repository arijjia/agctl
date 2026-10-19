package server

import (
	"net/http"
	"strings"
	"testing"
)

// TestApprovalFlow — режим require-approval: прямой create запрещён, заявка
// уходит в pending, админ одобряет → воркспейс создаётся и provision идёт;
// reject для второй заявки оставляет владельца без места; не-админ не может
// одобрять чужие заявки.
func TestApprovalFlow(t *testing.T) {
	e := newTestEnv(t, 10)
	e.srv.cfg.RequireApproval = true

	// админ (первый) и обычный пользователь
	e.register(e.admin, "root-admin", "root-admin-pass")
	bob := e.newClient()
	e.register(bob, "bob", "bob-pass-1")

	// прямой create в этом режиме запрещён
	code, body := e.do(bob, "POST", "/api/workspaces", validCreate("bob-direct"))
	if code != http.StatusForbidden || !strings.Contains(body, "апрув") {
		t.Fatalf("direct create: %d %s", code, body)
	}

	// заявка
	code, body = e.do(bob, "POST", "/api/workspace-requests", validCreate("bob-approved"))
	if code != http.StatusAccepted {
		t.Fatalf("request: %d %s", code, body)
	}

	// не-админ не может одобрять
	reqID := requestIDof(t, body)
	code, _ = e.do(bob, "POST", "/api/workspace-requests/"+reqID+"/approve", nil)
	if code != http.StatusForbidden {
		t.Fatalf("bob approve: %d, want 403", code)
	}

	// админ одобряет
	code, body = e.do(e.admin, "POST", "/api/workspace-requests/"+reqID+"/approve", nil)
	if code != http.StatusAccepted {
		t.Fatalf("admin approve: %d %s", code, body)
	}
	ws := e.waitStatus(wsIDof(t, body), "running")
	if ws.Name != "bob-approved" || ws.OwnerID == 0 {
		t.Fatalf("workspace from approval: %+v", ws)
	}

	// владелец видит место в своём списке
	_, list := e.do(bob, "GET", "/api/workspaces", nil)
	if !strings.Contains(list, "bob-approved") {
		t.Fatalf("workspace not in user list: %s", list)
	}

	// вторая заявка → reject
	code, body = e.do(bob, "POST", "/api/workspace-requests", validCreate("bob-rejected"))
	if code != http.StatusAccepted {
		t.Fatalf("second request: %d %s", code, body)
	}
	reqID2 := requestIDof(t, body)
	code, body = e.do(e.admin, "POST", "/api/workspace-requests/"+reqID2+"/reject",
		map[string]string{"reason": "квота исчерпана"})
	if code != 200 || !strings.Contains(body, "rejected") {
		t.Fatalf("reject: %d %s", code, body)
	}
	// повторная обработка запрещена
	code, _ = e.do(e.admin, "POST", "/api/workspace-requests/"+reqID2+"/approve", nil)
	if code != http.StatusConflict {
		t.Fatalf("double decision: %d, want 409", code)
	}
}

func requestIDof(t *testing.T, body string) string {
	t.Helper()
	marker := `"request":{"id":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("нет id заявки в ответе: %s", body)
	}
	rest := body[i+len(marker):]
	return rest[:strings.Index(rest, `"`)]
}
