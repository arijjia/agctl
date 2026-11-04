package server

// admin_test.go — админ-панель супер-админа (карточка admin-panel-super-admin):
// доступ по ролям, маршрутизация admin → /admin, журнал шлюза, квоты,
// сессии/траектории из тома.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/store"
	"github.com/klauspost/compress/zstd"
)

func TestAdminPanelAccessControl(t *testing.T) {
	env := newTestEnv(t, 2)
	env.register(env.admin, "anna", "secret1") // первый — admin
	user := env.newClient()
	if code := env.register(user, "alice", "alicepw1"); code != 200 {
		t.Fatalf("register alice: %d", code)
	}
	// аноним — 401
	if code, _ := env.do(env.newClient(), "GET", "/api/admin/overview", nil); code != 401 {
		t.Fatalf("anon overview: %d, ждём 401", code)
	}
	// обычный пользователь — 403
	for _, path := range []string{
		"/api/admin/overview", "/api/admin/users/details", "/api/admin/containers",
		"/api/admin/requests", "/api/admin/quotas", "/api/admin/users/2/sessions",
	} {
		if code, _ := env.do(user, "GET", path, nil); code != 403 {
			t.Fatalf("user %s: %d, ждём 403", path, code)
		}
	}
	// админ — 200
	for _, path := range []string{"/api/admin/overview", "/api/admin/users/details", "/api/admin/quotas"} {
		if code, _ := env.do(env.admin, "GET", path, nil); code != 200 {
			t.Fatalf("admin %s: %d, ждём 200", path, code)
		}
	}
}

func TestAdminRoleRouting(t *testing.T) {
	env := newTestEnvWith(t, 2, func(c *Config) { c.Tier0 = true }) // маршруты /chat/ и /
	env.register(env.admin, "anna", "secret1")
	// админ: корень → /admin
	req, _ := http.NewRequest("GET", env.api.URL+"/", nil)
	resp, err := env.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || resp.Header.Get("Location") != "/admin" {
		t.Fatalf("admin GET /: %d → %s, ждём 307 → /admin", resp.StatusCode, resp.Header.Get("Location"))
	}
	// админ: /chat/ отдаёт 403 (не HTML-запрос) — личный чат не выдаётся
	req2, _ := http.NewRequest("GET", env.api.URL+"/chat/", nil)
	resp2, err := env.admin.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("admin GET /chat/: %d, ждём 403", resp2.StatusCode)
	}
	// админ: /chat/ с Accept: text/html → редирект на /admin
	req3, _ := http.NewRequest("GET", env.api.URL+"/chat/", nil)
	req3.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp3, err := env.admin.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusTemporaryRedirect || resp3.Header.Get("Location") != "/admin" {
		t.Fatalf("admin GET /chat/ (html): %d → %s, ждём 307 → /admin",
			resp3.StatusCode, resp3.Header.Get("Location"))
	}
	// обычный пользователь: корень по-прежнему → /chat/
	alice := env.newClient()
	if code := env.register(alice, "alice", "alicepw1"); code != 200 {
		t.Fatalf("register alice: %d", code)
	}
	req4, _ := http.NewRequest("GET", env.api.URL+"/", nil)
	resp4, err := alice.Do(req4)
	if err != nil {
		t.Fatal(err)
	}
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusTemporaryRedirect || resp4.Header.Get("Location") != "/chat/" {
		t.Fatalf("user GET /: %d → %s, ждём 307 → /chat/", resp4.StatusCode, resp4.Header.Get("Location"))
	}
	// страница /admin отдаёт index.html SPA (доступна и не залогиненным —
	// авторизацию делает /api/me на клиенте и requireAdmin на API)
	req5, _ := http.NewRequest("GET", env.api.URL+"/admin", nil)
	resp5, err := env.newClient().Do(req5)
	if err != nil {
		t.Fatal(err)
	}
	resp5.Body.Close()
	if resp5.StatusCode != 200 {
		t.Fatalf("GET /admin: %d, ждём 200", resp5.StatusCode)
	}
}

func TestAdminQuotasCRUD(t *testing.T) {
	env := newTestEnv(t, 2)
	env.register(env.admin, "anna", "secret1")
	if code, _ := env.do(env.admin, "PUT", "/api/admin/quotas", map[string]any{
		"scope": "role", "subject": "developer", "dailyTokens": 100000, "monthlyTokens": 1000000, "note": "девы",
	}); code != 200 {
		t.Fatalf("quota put role: %d", code)
	}
	if code, _ := env.do(env.admin, "PUT", "/api/admin/quotas", map[string]any{
		"scope": "user", "subject": "alice", "dailyTokens": 500, "note": "личный лимит",
	}); code != 200 {
		t.Fatalf("quota put user: %d", code)
	}
	code, body := env.do(env.admin, "GET", "/api/admin/quotas", nil)
	if code != 200 {
		t.Fatalf("quota list: %d", code)
	}
	var resp struct {
		Quotas []store.Quota `json:"quotas"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode quotas: %v: %s", err, body)
	}
	if len(resp.Quotas) != 2 {
		t.Fatalf("ждём 2 квоты, есть %d: %s", len(resp.Quotas), body)
	}
	// персональная перекрывает ролевую
	q, err := env.db.QuotaEffectiveForUser("developer", "alice")
	if err != nil || q.Subject != "alice" || q.DailyTokens != 500 {
		t.Fatalf("quota effective: %+v err=%v, ждём личную 500", q, err)
	}
	// у кого нет персональной — ролевая
	q2, err := env.db.QuotaEffectiveForUser("developer", "bob")
	if err != nil || q2.Subject != "developer" || q2.DailyTokens != 100000 {
		t.Fatalf("quota effective role: %+v err=%v", q2, err)
	}
	// без квоты — пусто
	q3, err := env.db.QuotaEffectiveForUser("user", "nobody")
	if err != nil || q3.ID != 0 {
		t.Fatalf("quota empty: %+v err=%v, ждём пустую", q3, err)
	}
	// некорректный scope — 400
	if code, _ := env.do(env.admin, "PUT", "/api/admin/quotas", map[string]any{
		"scope": "world", "subject": "x",
	}); code != 400 {
		t.Fatalf("quota put bad scope: %d, ждём 400", code)
	}
	// некорректное тело (пустое) — 400
	if code, _ := env.do(env.admin, "PUT", "/api/admin/quotas", nil); code != 400 {
		t.Fatalf("quota put empty body: %d, ждём 400", code)
	}
	// удаление
	delID := resp.Quotas[0].ID
	if code, _ := env.do(env.admin, "DELETE", "/api/admin/quotas/"+strconv.FormatInt(delID, 10), nil); code != 200 {
		t.Fatalf("quota delete: %d", code)
	}
	if got, _ := env.db.QuotaList(); len(got) != 1 {
		t.Fatalf("после delete осталось %d квот", len(got))
	}
}

func TestAdminProxyRequestsJournal(t *testing.T) {
	env := newTestEnvWith(t, 2, func(c *Config) { c.Tier0 = true }) // маршруты /chat/
	env.register(env.admin, "anna", "secret1")
	alice := env.newClient()
	if code := env.register(alice, "alice", "alicepw1"); code != 200 {
		t.Fatalf("register: %d", code)
	}
	// админ ходит в /chat/ — в журнал шлюза НЕ попадает (личного чата нет)
	req, _ := http.NewRequest("GET", env.api.URL+"/chat/", nil)
	resp, err := env.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// пользователь открывает /chat/: ensureChat (fake) → прокси → статус
	req2, _ := http.NewRequest("GET", env.api.URL+"/chat/", nil)
	resp2, err := alice.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	code, body := env.do(env.admin, "GET", "/api/admin/requests?limit=100", nil)
	if code != 200 {
		t.Fatalf("requests list: %d", code)
	}
	var jr struct {
		Requests []store.ProxyRequest `json:"requests"`
	}
	if err := json.Unmarshal([]byte(body), &jr); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	if len(jr.Requests) == 0 {
		t.Fatalf("журнал пуст: %s", body)
	}
	foundAlice := false
	for _, r0 := range jr.Requests {
		if r0.Login == "alice" {
			foundAlice = true
		}
		if r0.Login == "anna" {
			t.Fatalf("админ попал в журнал шлюза: %+v", r0)
		}
	}
	if !foundAlice {
		t.Fatalf("запрос alice не найден в журнале: %s", body)
	}
	// фильтр по пользователю (alice = второй зарегистрированный = id 2)
	code, body = env.do(env.admin, "GET", "/api/admin/requests?user_id=2&limit=100", nil)
	if code != 200 || !strings.Contains(body, "alice") {
		t.Fatalf("фильтр user_id=2: %d %s", code, body)
	}
}

func TestAdminContainersDegrade(t *testing.T) {
	env := newTestEnv(t, 2)
	env.register(env.admin, "anna", "secret1")
	// fakeRuntime не реализует containerLister: список отдаёт 503,
	// операции по неизвестному имени — 404
	if code, _ := env.do(env.admin, "GET", "/api/admin/containers", nil); code != 503 {
		t.Fatalf("containers: %d, ждём 503", code)
	}
	if code, _ := env.do(env.admin, "POST", "/api/admin/containers/agctl-chat-x-u2/stop", nil); code != 404 {
		t.Fatalf("container stop: %d, ждём 404", code)
	}
	if code, _ := env.do(env.admin, "POST", "/api/admin/containers/other-ctr/stop", nil); code != 404 {
		t.Fatalf("чужое имя: %d, ждём 404", code)
	}
}

func TestAdminSessionsAndTrace(t *testing.T) {
	env := newTestEnv(t, 2)
	env.register(env.admin, "anna", "secret1")
	root := filepath.Join(env.home, "chats", "u2", "dsh-home", "sessions", "--workspace--", "session-abc123")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	// готовим zstd-JSONL как пишет dsh
	var buf bytes.Buffer
	zw, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = zw.Write([]byte("{\"type\":\"session\",\"version\":4,\"id\":\"session-abc123\"}\n{\"type\":\"turn\",\"n\":1}\n\n"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session.v4.jsonl.zstd"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	// сессии отсутствующего тома — пустой список, не ошибка
	code, body := env.do(env.admin, "GET", "/api/admin/users/9/sessions", nil)
	if code != 200 || !strings.Contains(body, "\"sessions\":") {
		t.Fatalf("sessions u9: %d %s", code, body)
	}
	// список по id
	code, body = env.do(env.admin, "GET", "/api/admin/users/2/sessions", nil)
	if code != 200 || !strings.Contains(body, "session-abc123") {
		t.Fatalf("sessions u2: %d %s", code, body)
	}
	// трейс декодируется
	code, body = env.do(env.admin, "GET",
		"/api/admin/users/2/sessions/--workspace--/session-abc123", nil)
	if code != 200 || !strings.Contains(body, "\"type\":\"turn\"") {
		t.Fatalf("trace: %d %s", code, body)
	}
	// path traversal отрезается
	code, _ = env.do(env.admin, "GET",
		"/api/admin/users/2/sessions/..%2F..%2Fother/session-abc123", nil)
	if code != 400 && code != 404 {
		t.Fatalf("traversal: %d, ждём 400/404", code)
	}
	// обычный пользователь не получает чужие сессии
	alice := env.newClient()
	if code := env.register(alice, "alice", "alicepw1"); code != 200 {
		t.Fatalf("register: %d", code)
	}
	if code, _ := env.do(alice, "GET", "/api/admin/users/2/sessions", nil); code != 403 {
		t.Fatalf("user sessions: %d, ждём 403", code)
	}
}

func TestAdminOverviewNumbers(t *testing.T) {
	env := newTestEnv(t, 2)
	env.register(env.admin, "anna", "secret1")
	alice := env.newClient()
	if code := env.register(alice, "alice", "alicepw1"); code != 200 {
		t.Fatalf("register: %d", code)
	}
	code, body := env.do(env.admin, "GET", "/api/admin/overview", nil)
	if code != 200 {
		t.Fatalf("overview: %d", code)
	}
	var ov struct {
		Users struct {
			Total  int            `json:"total"`
			ByRole map[string]int `json:"byRole"`
		} `json:"users"`
		Containers map[string]int `json:"containers"`
	}
	if err := json.Unmarshal([]byte(body), &ov); err != nil {
		t.Fatalf("decode: %v: %s", err, body)
	}
	if ov.Users.Total != 2 || ov.Users.ByRole["admin"] != 1 || ov.Users.ByRole["user"] != 1 {
		t.Fatalf("overview users: %+v (%s)", ov.Users, body)
	}
	if _, ok := ov.Containers["running"]; !ok {
		t.Fatalf("overview containers: %+v", ov.Containers)
	}
}
