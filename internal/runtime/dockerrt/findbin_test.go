package dockerrt

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindLinuxBinaryPicksDaemonArch(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "agctl-guest-linux-arm64")
	touch(t, dir, "agctl-guest-linux-amd64")
	for _, arch := range []string{"amd64", "arm64"} {
		got, err := findLinuxBinary(dir, arch)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "agctl-guest-linux-"+arch {
			t.Fatalf("arch %s: got %s", arch, got)
		}
	}
	if got, err := findLinuxBinary(dir, ""); err != nil || filepath.Base(got) != "agctl-guest-linux-arm64" {
		t.Fatalf("any arch: %s %v", got, err)
	}
}

// Имя guest одно — agctl-guest-linux-<arch>; прежние имена (единый бинарь
// agctl-linux-<arch>, имя из релиз-архива agctl-guest_linux_<arch>) больше
// не подхватываются. Раскладка корня: рядом с бинарём — каталог guest/.
func TestFindLinuxBinaryIgnoresLegacyNames(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "agctl-linux-arm64")
	touch(t, dir, "agctl-guest_linux_arm64")
	if p, err := findLinuxBinary(dir, "arm64"); err == nil && filepath.Dir(p) == dir {
		t.Fatalf("взято прежнее имя: %s", p)
	}
	touch(t, dir, "agctl-guest-server_linux_arm64")
	if p, err := findServerLinuxBinary(dir, "arm64"); err == nil && filepath.Dir(p) == dir {
		t.Fatalf("взято прежнее имя серверного guest: %s", p)
	}
	touch(t, dir, "agctl-guest-linux-arm64")
	if got, err := findLinuxBinary(dir, "arm64"); err != nil || filepath.Base(got) != "agctl-guest-linux-arm64" {
		t.Fatalf("guest: %s %v", got, err)
	}
}

func TestFindLinuxBinaryMissingArch(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "agctl-guest-linux-arm64")
	if p, err := findLinuxBinary(dir, "amd64"); err == nil && filepath.Dir(p) == dir {
		t.Fatalf("взяли чужую архитектуру: %s", p)
	}
}

// Серверный вариант: только agctl-guest-server-linux-<arch>; локальный бинарь
// (agctl-guest-linux-<arch> после разделения по режимам) серверу не подходит —
// в нём нет ax-runner/init-chat-home.
func TestFindServerLinuxBinary(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "agctl-guest-linux-arm64")
	if _, err := findServerLinuxBinary(dir, "arm64"); err == nil {
		t.Fatal("сервер взял локальный guest — нужен отказ")
	}
	touch(t, dir, "agctl-guest-server-linux-arm64")
	touch(t, dir, "agctl-guest-server-linux-amd64")
	for _, arch := range []string{"amd64", "arm64"} {
		got, err := findServerLinuxBinary(dir, arch)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "agctl-guest-server-linux-"+arch {
			t.Fatalf("arch %s: got %s", arch, got)
		}
	}
	if got, err := findServerLinuxBinary(dir, ""); err != nil || filepath.Base(got) != "agctl-guest-server-linux-arm64" {
		t.Fatalf("any arch: %s %v", got, err)
	}
}
