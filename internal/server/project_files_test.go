package server

// project_files_test.go — файлы воркспейса для пользователя: список и
// скачивание через gateway (артефакты агента забираются напрямую).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectFilesListAndDownload(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	user := env.newClient()
	env.register(user, "boris", "secret2")
	if code, _ := env.do(user, "POST", "/api/projects", map[string]string{"name": "deck"}); code != 201 {
		t.Fatal("create")
	}
	dir := projectDirOf(env, 2, "deck")
	write := func(rel, body string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("presentation/Pro_sebya.pptx", "PK\x03\x04 pptx-bytes")
	write("presentation/plan.json", "{}")
	write(".git/config", "[core]")              // служебный — в список не попадает
	write(".agctl/stash/skill/x/SKILL.md", "s") // и stash

	// список: артефакты видны, служебные каталоги скрыты
	code, body := env.do(user, "GET", "/api/projects/deck/files", nil)
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	if !contains(body, "presentation/Pro_sebya.pptx") || !contains(body, "presentation/plan.json") {
		t.Fatalf("артефактов нет в списке: %s", body)
	}
	if contains(body, ".git") || contains(body, ".agctl") {
		t.Fatalf("служебные каталоги в списке: %s", body)
	}

	// скачивание своего файла
	code, body = env.do(user, "GET", "/api/projects/deck/file?path=presentation%2FPro_sebya.pptx", nil)
	if code != 200 || body != "PK\x03\x04 pptx-bytes" {
		t.Fatalf("download: %d %q", code, body)
	}

	// traversal-попытки
	for _, bad := range []string{"..%2F..%2F.env", "%2e%2e/x"} {
		if code, _ := env.do(user, "GET", "/api/projects/deck/file?path="+bad, nil); code == 200 {
			t.Fatalf("traversal %s прошёл", bad)
		}
	}
	// чужой воркспейс не адресуем
	if code, _ := env.do(env.admin, "GET", "/api/projects/deck/files", nil); code != 404 {
		t.Fatalf("чужой list: %d, ждём 404", code)
	}
	// несуществующий файл — 404
	if code, _ := env.do(user, "GET", "/api/projects/deck/file?path=nope.pptx", nil); code != 404 {
		t.Fatalf("missing file: %d", code)
	}
	// каталог как файл — 400
	if code, _ := env.do(user, "GET", "/api/projects/deck/file?path=presentation", nil); code != 400 {
		t.Fatalf("dir download: %d, ждём 400", code)
	}
}

// TestProjectFilesNameHeader — UTF-8 имя файла уходит в Content-Disposition.
func TestProjectFilesNameHeader(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	if code, _ := env.do(env.admin, "POST", "/api/projects", map[string]string{"name": "hdr"}); code != 201 {
		t.Fatal("create")
	}
	dir := projectDirOf(env, 1, "hdr")
	name := "О_себе.pptx"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("GET", env.api.URL+"/api/projects/hdr/file?path="+strings.ReplaceAll(name, " ", "%20"), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := env.admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Fatalf("Content-Disposition без RFC5987: %q", cd)
	}
}
