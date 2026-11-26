package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/provision"
	"gopkg.in/yaml.v3"
)

// Правки моделей из настроек харнесса доезжают до хоста: новый провайдер
// становится личным (с ключом), ключ провайдера организации обновляется в
// хранилище, а правка, сделанная на хосте, обратно не едет.
func TestPullHarnessLLM(t *testing.T) {
	env, base := newLocalUIEnv(t)
	h := &providerHooks{fakeHooks: base, keys: map[string]string{}}
	env.srv.cfg.Local.Hooks = h
	env.srv.cfg.DefaultLLM = provision.Provider{Name: "corp", BaseURL: "https://llm.corp.invalid/v1",
		Models: []string{"fast", "deep"}, APIKey: "corp-key-1"}

	home := filepath.Join(t.TempDir(), "dsh-home")
	if err := os.MkdirAll(filepath.Join(home, "profiles", "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	patch := filepath.Join(home, "profiles", "web", "cordis.patch.yml")
	if err := os.WriteFile(patch, []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := provision.RefreshProvider(home, env.srv.defaultLLM()); err != nil {
		t.Fatal(err)
	}
	if env.srv.pullHarnessLLM(home) {
		t.Fatal("сразу после записи agctl правок харнесса быть не должно")
	}

	// «Пользователь в dsh»: добавил провайдера My_Route и сменил ключ corp.
	raw, _ := os.ReadFile(patch)
	var rows []map[string]any
	if err := yaml.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	patched := false
	for _, row := range rows {
		if row["name"] != "@deepseek-ai/dsh-llm-pi-ai" {
			continue
		}
		providers := row["config"].(map[string]any)["providers"].(map[string]any)
		providers["My_Route"] = map[string]any{
			"api": "openai-completions", "apiKeyEnv": "MY_ROUTE_API_KEY", "baseURL": "https://my.example.invalid/v1",
			"models": []any{map[string]any{"id": "m1", "name": "m1"}},
		}
		patched = true
	}
	if !patched {
		t.Fatalf("в патче нет строки llm-pi-ai:\n%s", raw)
	}
	out, _ := yaml.Marshal(rows)
	if err := os.WriteFile(patch, out, 0o644); err != nil {
		t.Fatal(err)
	}
	creds := "version: 1\nrefs:\n  AGCTL_LLM_API_KEY: corp-key-2\n  MY_ROUTE_API_KEY: my-route-key\n"
	if err := os.WriteFile(filepath.Join(home, ".credentials.yaml"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}

	if !env.srv.pullHarnessLLM(home) {
		t.Fatal("правки харнесса не замечены")
	}
	own := h.Providers()
	if len(own) != 1 || own[0].Name != "my-route" || own[0].BaseURL != "https://my.example.invalid/v1" || len(own[0].Models) != 1 {
		t.Fatalf("личный провайдер из харнесса: %+v", own)
	}
	if got := h.ModelKey(localmode.ProviderKeyEnv("my-route")); got != "my-route-key" {
		t.Fatalf("ключ личного провайдера: %q", got)
	}
	if got := base.LLMKey(); got != "corp-key-2" {
		t.Fatalf("ключ провайдера организации из харнесса: %q", got)
	}
	if env.srv.pullHarnessLLM(home) {
		t.Fatal("одна и та же правка перенесена дважды")
	}
}
