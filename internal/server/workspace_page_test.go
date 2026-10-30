package server

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/store"
)

// TestInjectDevShell — dev-шелл брендинга встаёт после <head>, идемпотентен и
// содержит логотип/бренд, но не трогает чат-специфику (нет скрытия панелей).
func TestInjectDevShell(t *testing.T) {
	brand := fixtureView(t)
	srv := &Server{brand: brand}
	doc := `<!doctype html><html><head><title>DeepSeek Harness</title></head><body>ok</body></html>`
	out := string(srv.injectDevShell([]byte(doc)))
	if !strings.Contains(out, `<style data-agctl="dev-shell">`) {
		t.Fatal("dev-shell не вставлен")
	}
	if !strings.Contains(out, "<title>"+html.EscapeString(brand.Title)+"</title>") {
		t.Fatal("title не заменён на wire")
	}
	if !strings.Contains(out, brand.LogoDataURI) {
		t.Fatal("логотип профиля не вшит в dev-шелл")
	}
	if !strings.Contains(out, brand.Tagline) {
		t.Fatal("тэглайн не заменяется dev-шеллом")
	}
	again := string(srv.injectDevShell([]byte(out)))
	if again != out {
		t.Fatal("инъекция не идемпотентна")
	}
}

// TestInjectDevShellNeutralWithoutLogo — профиль без логотипа использует
// букву-знак вместо data-URI, бренд остаётся нейтральным.
func TestInjectDevShellNeutralWithoutLogo(t *testing.T) {
	srv := &Server{brand: profile.Neutral()}
	doc := `<!doctype html><html><head><title>DeepSeek Harness</title></head><body>ok</body></html>`
	out := string(srv.injectDevShell([]byte(doc)))
	if strings.Contains(out, "data:image/png;base64,") {
		t.Fatal("нейтральный профиль не должен вшивать логотип")
	}
	if !strings.Contains(out, `"a"`) || !strings.Contains(out, `"agctl"`) {
		t.Fatal("буква-знак/имя agctl не подставлены в dev-шелл")
	}
}

// TestStoppedWorkspacePage — страница остановленного места брендируется,
// экранирует имя и несёт кнопку запуска с polling на API места.
func TestStoppedWorkspacePage(t *testing.T) {
	brand := fixtureView(t)
	ws := store.Workspace{ID: `w1"><script>`, Name: `Место "тест"`, Status: "stopped"}
	page := stoppedWorkspacePage(ws, brand)
	for _, want := range []string{
		brand.Name,
		brand.LogoDataURI,
		"Рабочее место «Место &#34;тест&#34;» сейчас остановлено",
		"agctl-start",
		"/api/workspaces/w1%22%3E%3Cscript%3E/start",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("в странице нет %q", want)
		}
	}
	if strings.Contains(page, `w1"><script>`) {
		t.Fatal("id воркспейса не экранирован в URL")
	}
	// Нейтральный профиль: имя agctl, буква-знак вместо логотипа, ничего чужого.
	neutral := stoppedWorkspacePage(ws, profile.Neutral())
	if !strings.Contains(neutral, "agctl") || strings.Contains(neutral, "data:image/png") {
		t.Fatal("нейтральная страница останова брендирована неверно")
	}
	if strings.Contains(neutral, brand.Name) {
		t.Fatal("нейтральная страница содержит бренд организации")
	}
}

// TestReconcileUnhealthyWorkspace — reconcile помечает stopped dev-место,
// чей контейнер Docker считает unhealthy (dsh завис, healthcheck не отвечает).
func TestReconcileUnhealthyWorkspace(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	ws := store.Workspace{ID: "wun", OwnerID: 1, Name: "hangs", Status: "running",
		ContainerID: "ctr-wun", BaseURL: "https://llm.example/v1", Model: "m"}
	if err := e.db.InsertWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	e.rt.health["ctr-wun"] = "unhealthy"
	oldGrace := reconcileGrace
	reconcileGrace = 0
	defer func() { reconcileGrace = oldGrace }()
	e.srv.reconcile()
	w, err := e.db.WorkspaceByID("wun")
	if err != nil {
		t.Fatal(err)
	}
	if w.Status != "stopped" {
		t.Fatalf("status = %q, want stopped (unhealthy)", w.Status)
	}
	// живой контейнер без health-проблем остаётся running
	e.rt.health["ctr-wun"] = "healthy"
	_ = e.db.UpdateWorkspaceStatus("wun", "running", "")
	e.srv.reconcile()
	if w, _ = e.db.WorkspaceByID("wun"); w.Status != "running" {
		t.Fatalf("healthy-контейнер не должен помечаться stopped: %q", w.Status)
	}
}

// TestProxyWorkspaceStoppedPage — браузер по /w/{id}/ остановленного места
// получает брендированную HTML-страницу с кнопкой запуска, API-клиент —
// прежний JSON. владелец места — авторизованный пользователь.
func TestProxyWorkspaceStoppedPage(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")

	ws := store.Workspace{ID: "wstop", OwnerID: 1, Name: "останавливалось",
		Status: "stopped", BaseURL: "https://llm.example/v1", Model: "m1"}
	if err := e.db.InsertWorkspace(ws); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}

	// Браузерный запрос: Accept требует text/html → HTML-страница с кнопкой.
	req, err := http.NewRequest("GET", e.api.URL+"/w/wstop/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := e.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, resp)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("html: статус %d, тело: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("html: content-type %q", ct)
	}
	for _, want := range []string{"agctl-start", e.srv.brand.Name, "/api/workspaces/wstop/start"} {
		if !strings.Contains(body, want) {
			t.Fatalf("html: в странице нет %q", want)
		}
	}

	// API-клиент: без text/html в Accept — прежний JSON.
	req2, err := http.NewRequest("GET", e.api.URL+"/w/wstop/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req2.Header.Set("Accept", "application/json")
	resp2, err := e.admin.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	body2 := readAll(t, resp2)
	if resp2.StatusCode != http.StatusConflict || !strings.Contains(body2, "воркспейс не запущен") {
		t.Fatalf("json: статус %d, тело: %s", resp2.StatusCode, body2)
	}
}
