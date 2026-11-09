package server

// dz_defect_d9_symlink_test.go — красно-зелёный цикл дефекта D9 (ДЗ РТиППО):
// скачивание файла проекта обязано отвергать симлинк, ведущий за пределы
// тома пользователя. Красная фаза зафиксирована 09.11.2026 (os.Stat следовал
// по ссылке и отдавал содержимое файла хоста).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDZFileDownloadRejectsSymlinkEscape — симлинк внутри проекта, ведущий
// на файл ВНЕ дома agctl, не должен скачиваться (ожидаем 4xx и отсутствие
// содержимого). Красная фаза: os.Stat следует по ссылке и отдаёт файл.
func TestDZFileDownloadRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("создание симлинков на Windows требует привилегий разработчика")
	}
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	if code, _ := env.do(env.admin, "POST", "/api/projects", map[string]string{"name": "sym"}); code != 201 {
		t.Fatalf("создание проекта: %d", code)
	}
	dir := projectDirOf(env, 1, "sym")
	secret := filepath.Join(t.TempDir(), "host-secret.txt")
	if err := os.WriteFile(secret, []byte("HOST-SECRET-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	code, body := env.do(env.admin, "GET", "/api/projects/sym/file?path=leak.txt", nil)
	if code == 200 {
		t.Fatalf("D9: симлинк на файл хоста скачан (%d, тело %q) — Stat следует по ссылке, гард отсутствует", code, body)
	}
	if strings.Contains(body, "HOST-SECRET-CONTENT") {
		t.Fatalf("D9: содержимое файла хоста утекло в ответе: %q", body)
	}
}
