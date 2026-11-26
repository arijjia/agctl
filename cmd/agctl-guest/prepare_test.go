package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"0.2.2-rc.5", "0.2.2-rc.3", 1},
		{"0.2.3-alpha.2", "0.2.2-rc.5", 1},
		{"0.2.2-rc.5", "0.2.3-alpha.2", -1},
		{"0.2.2", "0.2.2-rc.5", 1},
		{"0.2.2-beta.1", "0.2.2-alpha.9", 1},
		{"0.2.2-rc.5", "0.2.2-rc.5", 0},
		{"0.2.2-rc.5", "", 1},
	} {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("%s vs %s = %d, нужно %d", c.a, c.b, got, c.want)
		}
	}
}

// Метка версий dsh-home: первая метка, апгрейд, откат с предупреждением,
// история смен; без package.json dsh — предупреждение, без падения.
func TestPrepareHome(t *testing.T) {
	home := t.TempDir()
	pkg := filepath.Join(t.TempDir(), "package.json")
	setDsh := func(v string) {
		if err := os.WriteFile(pkg, []byte(`{"version":"`+v+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	setDsh("0.2.2-rc.5")
	if out := strings.Join(prepareHome(home, pkg, "0.1.0 abc", now), "\n"); !strings.Contains(out, "первая метка") {
		t.Fatalf("первый старт: %s", out)
	}
	setDsh("0.2.3-alpha.2")
	if out := strings.Join(prepareHome(home, pkg, "0.1.0 abc", now), "\n"); !strings.Contains(out, "апгрейд dsh 0.2.2-rc.5 → 0.2.3-alpha.2") {
		t.Fatalf("апгрейд: %s", out)
	}
	setDsh("0.2.2-rc.5")
	if out := strings.Join(prepareHome(home, pkg, "0.1.0 abc", now), "\n"); !strings.Contains(out, "⚠ откат dsh 0.2.3-alpha.2 → 0.2.2-rc.5") {
		t.Fatalf("откат: %s", out)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".agctl", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st homeState
	if err := json.Unmarshal(raw, &st); err != nil || st.Dsh != "0.2.2-rc.5" || len(st.History) != 2 || st.Guest != "0.1.0 abc" {
		t.Fatalf("метка: %+v %v", st, err)
	}
	if out := prepareHome(home, filepath.Join(t.TempDir(), "нет.json"), "", now); !strings.Contains(out[0], "не определена") {
		t.Fatalf("без dsh: %v", out)
	}
}
