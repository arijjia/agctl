package dockerrt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindLinuxBinaryPicksDaemonArch(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"agctl-linux-arm64", "agctl-linux-amd64"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		got, err := findLinuxBinary(dir, arch)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != "agctl-linux-"+arch {
			t.Fatalf("arch %s: got %s", arch, got)
		}
	}
	if got, err := findLinuxBinary(dir, ""); err != nil || filepath.Base(got) != "agctl-linux-arm64" {
		t.Fatalf("any arch: %s %v", got, err)
	}
}

func TestFindLinuxBinaryMissingArch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agctl-linux-arm64"), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Тестовый бинарь лежит не рядом с dir, а linux amd64 «сам» подойти может
	// только на linux/amd64 — там проверка неинформативна.
	if p, err := findLinuxBinary(dir, "amd64"); err == nil && filepath.Dir(p) == dir {
		t.Fatalf("взяли чужую архитектуру: %s", p)
	}
}
