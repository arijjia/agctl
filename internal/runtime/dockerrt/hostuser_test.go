package dockerrt

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Запись пользователя — второй строкой (раньше node с тем же uid 1000), своя
// группа — только если такого gid в образе нет.
func TestUserFiles(t *testing.T) {
	passwd, group := userFiles(1000, 1000)
	lines := strings.Split(passwd, "\n")
	if lines[1] != "dsh:x:1000:1000:agctl:/workspace:/bin/bash" {
		t.Fatalf("вторая строка passwd: %q", lines[1])
	}
	if strings.Contains(group, "dsh:x:1000:") {
		t.Fatal("группа 1000 есть в образе — своя не нужна")
	}
	_, group = userFiles(123456, 54321)
	if !strings.HasSuffix(group, "dsh:x:54321:\n") {
		t.Fatalf("нет группы dsh: %q", group[len(group)-40:])
	}
	if p, _ := userFiles(0, 0); p != imagePasswd {
		t.Fatal("root — passwd образа как есть")
	}
	for _, bad := range []string{"", "1000", "a:b", "-1:2"} {
		if _, _, err := parseUser(bad); err == nil {
			t.Errorf("%q принят", bad)
		}
	}
	dir := t.TempDir()
	binds, err := writeUserFiles(filepath.Join(dir, "etc"), 1234, 1234)
	if err != nil || len(binds) != 2 || !strings.HasSuffix(binds[0], ":/etc/passwd:ro") {
		t.Fatalf("%v %v", binds, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "etc", "passwd")); !strings.Contains(string(b), "dsh:x:1234:1234:") {
		t.Fatal("passwd не записан")
	}
	if runtime.GOOS == "windows" {
		return // владельцев на Windows переводит Docker Desktop
	}
	if foreignOwned(dir, os.Getuid()) {
		t.Fatal("свои файлы считаются чужими")
	}
	if !foreignOwned(dir, os.Getuid()+1) {
		t.Fatal("чужой владелец не замечен")
	}
}
