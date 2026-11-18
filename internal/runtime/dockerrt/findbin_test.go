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

// Сборка единого бинаря до разделения (agctl-linux-<arch>) подходит, но
// agctl-guest предпочтительнее.
func TestFindLinuxBinaryPrefersGuestOverLegacy(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "agctl-linux-arm64")
	if got, err := findLinuxBinary(dir, "arm64"); err != nil || filepath.Base(got) != "agctl-linux-arm64" {
		t.Fatalf("legacy: %s %v", got, err)
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
