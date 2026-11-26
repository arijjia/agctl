package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

// Редактор профиля пишет profile.yaml в каталог профиля на диске: проверка
// теми же правилами, что и загрузка, прежний текст остаётся рядом, id
// менять нельзя; встроенный профиль не редактируется.
func TestProfileSourceEditor(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)

	// Встроенный профиль: только чтение, с объяснением.
	code, body := env.do(c, "GET", "/api/profile/source", nil)
	if code != http.StatusOK || !strings.Contains(body, `"writable":false`) {
		t.Fatalf("встроенный профиль: %d %s", code, body)
	}

	dir := filepath.Join(t.TempDir(), "acme")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "# комментарий организации\nschema: 1\nid: acme\nbrand:\n  name: Acme\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := profile.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	env.srv.cfg.Profile = prof

	code, body = env.do(c, "GET", "/api/profile/source", nil)
	if code != http.StatusOK || !strings.Contains(body, `"writable":true`) || !strings.Contains(body, "комментарий организации") {
		t.Fatalf("дисковый профиль: %d %s", code, body)
	}
	put := func(text string) (int, string) {
		req, _ := http.NewRequest("PUT", env.api.URL+"/api/profile/source", strings.NewReader(text))
		req.Header.Set("Content-Type", "text/yaml")
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := os.ReadFile(filepath.Join(dir, "profile.yaml"))
		return resp.StatusCode, string(raw)
	}
	if code, disk := put("schema: 1\nid: other\nbrand:\n  name: X\n"); code != http.StatusBadRequest || disk != src {
		t.Fatalf("смена id должна быть отклонена, файл цел: %d\n%s", code, disk)
	}
	if code, disk := put("schema: 1\nid: acme\nbrand:\n  name: [\n"); code != http.StatusBadRequest || disk != src {
		t.Fatalf("битый YAML должен быть отклонён: %d", code)
	}
	next := "# комментарий организации\nschema: 1\nid: acme\nbrand:\n  name: Acme Workspaces\n  tagline: Привет\n"
	if code, disk := put(next); code != http.StatusOK || disk != next {
		t.Fatalf("сохранение: %d\n%s", code, disk)
	}
	if bak, _ := os.ReadFile(filepath.Join(dir, "profile.yaml.bak")); string(bak) != src {
		t.Fatalf("прежний текст не сохранён рядом: %q", bak)
	}
}
