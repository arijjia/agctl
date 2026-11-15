package profile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDataDir(t *testing.T) {
	home := func() (string, error) { return "/home/u", nil }
	noHome := func() (string, error) { return "", errors.New("нет дома") }
	getenv := func(env map[string]string) func(string) string {
		return func(k string) string { return env[k] }
	}

	cases := []struct {
		name string
		goos string
		env  map[string]string
		home func() (string, error)
		want string
	}{
		{"unix: XDG сильнее дома", "linux", map[string]string{"XDG_DATA_HOME": "/xdg"}, home, "/xdg/agctl/profiles"},
		{"unix: дом по умолчанию", "linux", map[string]string{}, home, "/home/u/.local/share/agctl/profiles"},
		{"macOS: как в install.sh", "darwin", map[string]string{}, home, "/home/u/.local/share/agctl/profiles"},
		{"windows: LOCALAPPDATA как в install.ps1", "windows", map[string]string{"LOCALAPPDATA": `C:\u\L`}, home, filepath.Join(`C:\u\L`, "agctl", "profiles")},
		{"windows: fallback на дом", "windows", map[string]string{}, home, filepath.Join("/home/u", "AppData", "Local", "agctl", "profiles")},
		{"windows: нет дома — пусто", "windows", map[string]string{}, noHome, ""},
		{"unix: нет дома — пусто", "linux", map[string]string{}, noHome, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, want := dataDir(c.goos, getenv(c.env), c.home), c.want; got != want {
				t.Fatalf("dataDir(%s): %q, ждём %q", c.goos, got, want)
			}
		})
	}
}
