package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doWith — запрос с заголовками (место запроса: X-Agctl-Place, Referer).
func (e *testEnv) doWith(c *http.Client, method, path string, body any, headers map[string]string) (int, string) {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, e.api.URL+path, bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

// Локальный режим: «Новый воркспейс» создаёт папку в папке рабочего места,
// из которого пришёл запрос (заголовок X-Agctl-Place или Referer /p/<id>/),
// без них — в папке основного места; «Возможности» находят проект там же.
func TestLocalProjectsFollowPlace(t *testing.T) {
	env, _ := newLocalUIEnv(t)
	c := loginOwner(t, env)
	dirs := mkdirs(t, t.TempDir(), "src2")
	if code, body := env.do(c, "POST", "/api/local/ui/places", map[string]any{
		"name": "Исходники", "id": "src2", "mounts": []map[string]string{{"host": dirs[0]}},
	}); code != http.StatusCreated {
		t.Fatalf("место: %d %s", code, body)
	}
	created := func(headers map[string]string, name string) map[string]any {
		t.Helper()
		code, body := env.doWith(c, "POST", "/api/projects", map[string]any{"name": name}, headers)
		if code != http.StatusCreated && code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, code, body)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(body), &out)
		return out["project"].(map[string]any)
	}
	p := created(map[string]string{placeHeader: "src2"}, "ws1")
	if p["directory"] != "/workspace/ws1" {
		t.Fatalf("путь в контейнере места: %v", p)
	}
	if fi, err := os.Stat(filepath.Join(dirs[0], "ws1")); err != nil || !fi.IsDir() {
		t.Fatalf("папка не в месте src2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.srv.localWorkDir(), "ws1")); !os.IsNotExist(err) {
		t.Fatal("папка создана в основном месте")
	}
	// Referer тоже указывает место
	created(map[string]string{"Referer": env.api.URL + "/p/src2/c/abc"}, "ws2")
	if _, err := os.Stat(filepath.Join(dirs[0], "ws2")); err != nil {
		t.Fatalf("по Referer: %v", err)
	}
	// без места — основное
	created(nil, "ws3")
	if _, err := os.Stat(filepath.Join(env.srv.localWorkDir(), "ws3")); err != nil {
		t.Fatalf("основное место: %v", err)
	}
	// «Возможности» ищут проект в папке места
	if code, body := env.doWith(c, "GET", "/api/projects/ws1/components", nil, map[string]string{placeHeader: "src2"}); code != http.StatusOK {
		t.Fatalf("components в месте: %d %s", code, body)
	}
	if code, _ := env.doWith(c, "GET", "/api/projects/ws1/components", nil, nil); code != http.StatusNotFound {
		t.Fatalf("в основном месте ws1 нет: %d", code)
	}
	// неизвестное место — ошибка, а не тихое основное
	if code, body := env.doWith(c, "POST", "/api/projects", map[string]any{"name": "ws4"}, map[string]string{placeHeader: "nope"}); code != http.StatusBadRequest || !strings.Contains(body, "не найдено") {
		t.Fatalf("чужое место: %d %s", code, body)
	}
}
