package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/auth"
)

// Новая установка: владелец — не учётка (нет строки в users), роль developer,
// сессия — в файле 0600 без самих токенов.
func TestLocalOwnerIsNotAnAccount(t *testing.T) {
	env, _ := newLocalEnv(t)
	if _, err := env.db.UserByLogin("owner"); err == nil {
		t.Fatal("в users появилась строка владельца")
	}
	if n, _ := env.db.CountUsers(); n != 0 {
		t.Fatalf("users не пуст: %d", n)
	}
	if env.srv.local.ownerID != localOwnerID {
		t.Fatalf("ownerID = %d, ждали %d", env.srv.local.ownerID, localOwnerID)
	}
	c := env.newClient()
	if code, _ := env.do(c, "GET", "/local/login?token=login-tok", nil); code != http.StatusSeeOther {
		t.Fatalf("вход: %d", code)
	}
	env.waitLocalWarm() // вход прогревает рабочее место в фоне
	code, body := env.do(c, "GET", "/api/me", nil)
	if code != 200 || !strings.Contains(body, `"role":"developer"`) {
		t.Fatalf("/api/me: %d %s", code, body)
	}
	if env.srv.userRole(localOwnerID) != "developer" {
		t.Fatal("роль владельца не developer")
	}

	path := filepath.Join(env.home, ownerSessionsFile)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("файл сессий: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("права файла сессий %o", st.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	for _, ck := range c.Jar.Cookies(mustURL(t, env.api.URL)) {
		if strings.Contains(string(raw), ck.Value) {
			t.Fatal("токен сессии лежит в файле открытым текстом")
		}
	}

	// пароля нет: вход по логину/паролю невозможен
	if code, _ := env.do(env.newClient(), "POST", "/api/login", map[string]string{"login": "owner", "password": "x"}); code == 200 {
		t.Fatal("вход по паролю прошёл")
	}

	// выход гасит сессию
	if code, _ := env.do(c, "POST", "/api/logout", nil); code >= 300 {
		t.Fatalf("logout: %d", code)
	}
	if code, _ := env.do(c, "GET", "/api/me", nil); code != http.StatusUnauthorized {
		t.Fatalf("после logout /api/me: %d", code)
	}
}

// Сессия владельца переживает рестарт процесса (dev update, dev up): файл
// перечитывается; истёкшая — не принимается.
func TestOwnerSessionsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ownerSessionsFile)
	a := loadOwnerSessions(path)
	if err := a.set("tok-live", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := a.set("tok-old", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	b := loadOwnerSessions(path)
	if !b.valid("tok-live") {
		t.Fatal("живая сессия потерялась при перечитывании")
	}
	if b.valid("tok-old") || b.valid("tok-unknown") {
		t.Fatal("принята истёкшая или чужая сессия")
	}
	if err := b.delete("tok-live"); err != nil || loadOwnerSessions(path).valid("tok-live") {
		t.Fatal("удалённая сессия жива")
	}
}

// Установка до этой модели: строка владельца в users уже есть — берётся её
// ID (тот же chats/u<ID> с сессиями dsh), новых строк не создаётся.
func TestLocalOwnerKeepsLegacyRowID(t *testing.T) {
	env, lm := newLocalEnv(t)
	hash, err := auth.HashPassword("legacy-random-password")
	if err != nil {
		t.Fatal(err)
	}
	// пара «чужих» строк, чтобы ID владельца отличался от localOwnerID
	if _, err := env.db.CreateUser("someone", hash, "user"); err != nil {
		t.Fatal(err)
	}
	legacyID, err := env.db.CreateUser(lm.OwnerLogin, hash, "developer")
	if err != nil {
		t.Fatal(err)
	}
	if got := env.srv.localOwnerIDFor(lm.OwnerLogin); got != legacyID {
		t.Fatalf("ownerID = %d, ждали ID прежней строки %d", got, legacyID)
	}
	if got := env.srv.localOwnerIDFor("nobody"); got != localOwnerID {
		t.Fatalf("новая установка: %d", got)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
