package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Модели сабагентов из профиля попадают в настройку dsh
// subagent-model-selection-settings: «модель» — у основного провайдера,
// «алиас/модель» — у дополнительного; неизвестные модели и дубли отсеяны.
func TestRefreshProviderSubagentModels(t *testing.T) {
	home := t.TempDir()
	p := Provider{
		Name: "zai", BaseURL: "https://api.example.org/v4", Model: "big", Models: []string{"big", "fast"},
		Extras:         []ExtraProvider{{Name: "corp", BaseURL: "https://llm.corp.example/v1", Models: []string{"corp-fast"}, APIKeyEnv: "CORP_KEY"}},
		SubagentModels: []string{"fast", "corp/corp-fast", "fast", "nope", "corp/absent"},
	}
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	want := "subagent-model-selection-settings:\n  enabled: true\n  allowedModels:\n" +
		"    - provider: zai\n      model: \"fast\"\n" +
		"    - provider: corp\n      model: \"corp-fast\"\n"
	if !strings.Contains(text, want) {
		t.Fatalf("settings.yaml без маршрутов сабагентов:\n%s", text)
	}
	if strings.Contains(text, "nope") || strings.Contains(text, "absent") {
		t.Fatalf("неизвестные модели не должны попасть в политику:\n%s", text)
	}

	// без списка — настройки нет: сабагенты на модели родителя
	p.SubagentModels = nil
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(home, "settings.yaml"))
	if strings.Contains(string(raw), "subagent-model-selection-settings") {
		t.Fatalf("без subagent_models настройки быть не должно:\n%s", raw)
	}
}
