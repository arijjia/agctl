package main

import (
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

func TestParseRuntimeKind(t *testing.T) {
	for raw, want := range map[string]string{"": "docker", "Docker": "docker", "k8s": "k8s", "kubernetes": "k8s", "ax": "ax", "substrate": "ax"} {
		got, err := parseRuntimeKind(raw)
		if err != nil || got != want {
			t.Fatalf("parseRuntimeKind(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := parseRuntimeKind("podman"); err == nil {
		t.Fatal("unknown runtime must be rejected")
	}
}

func TestParseLabels(t *testing.T) {
	got := parseLabels(" app.kubernetes.io/name=agctl , tier=gateway,,broken")
	if len(got) != 2 || got["app.kubernetes.io/name"] != "agctl" || got["tier"] != "gateway" {
		t.Fatalf("parseLabels = %v", got)
	}
	if parseLabels("") != nil {
		t.Fatal("empty selector must be nil (any pod)")
	}
}

func TestAXRunnerTokenPersistsAcrossRestarts(t *testing.T) {
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "agctl.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keyPath, err := secrets.EnsureMasterKey(home)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := axRunnerToken(db, sec)
	if err != nil || len(first) != 64 {
		t.Fatalf("token = %q, %v", first, err)
	}
	second, err := axRunnerToken(db, sec)
	if err != nil || second != first {
		t.Fatalf("token must be stable across restarts: %q != %q (%v)", first, second, err)
	}
	raw, _ := db.SettingGet(axRunnerTokenKey)
	if raw == first {
		t.Fatal("token must be stored encrypted")
	}
}
