package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
)

func TestInitChatHomeSeedsThenPreservesOrRefreshesProvider(t *testing.T) {
	home := t.TempDir()
	first := provision.Provider{BaseURL: "https://llm.example.invalid/v1", Model: "model-a"}
	seeded, refreshed, err := initChatHome(home, first, false, assets.DshSkel)
	if err != nil || !seeded || refreshed {
		t.Fatalf("seed result = (%v,%v,%v)", seeded, refreshed, err)
	}
	settingsPath := filepath.Join(home, "settings.yaml")
	firstSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	second := provision.Provider{BaseURL: "https://llm.example.invalid/v2", Model: "model-b"}
	seeded, refreshed, err = initChatHome(home, second, false, assets.DshSkel)
	if err != nil || seeded || refreshed {
		t.Fatalf("preserve result = (%v,%v,%v)", seeded, refreshed, err)
	}
	preserved, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(preserved) != string(firstSettings) {
		t.Fatal("existing provider configuration changed without refresh")
	}

	seeded, refreshed, err = initChatHome(home, second, true, assets.DshSkel)
	if err != nil || seeded || !refreshed {
		t.Fatalf("refresh result = (%v,%v,%v)", seeded, refreshed, err)
	}
	updated, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "model-b") || !strings.Contains(string(updated), "https://llm.example.invalid/v2") {
		t.Fatalf("provider settings were not refreshed: %s", updated)
	}
	credentials, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(credentials), "AGCTL_LLM_API_KEY:") {
		t.Fatalf("empty provider key must not be persisted: %s", credentials)
	}
}
