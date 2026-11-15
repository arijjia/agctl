package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
)

// TestDevHooksEntriesExtraProviders — личные ключи доп. провайдеров профиля
// (extra_providers с api_key_env) видны в «Доступах» даже без заданного
// значения: подписка «провайдер + ключ».
func TestDevHooksEntriesExtraProviders(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "profiles", "corp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema: 1\nid: corp\nbrand:\n  name: U\n  letter: U\nllm:\n  default_base_url: https://z.ai/v4\n  default_model: glm-5.3\n  default_models: glm-5.3\n  extra_providers:\n    - name: corp-litellm\n      base_url: https://llm.corp.example/v1\n      model: qwen-3.8-fast\n      models: qwen-3.8-deep,qwen-3.8-fast\n      api_key_env: AGCTL_LLM_CORP_API_KEY\n"
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := profile.LoadDir(filepath.Join(home, "profiles", "corp"))
	if err != nil {
		t.Fatal(err)
	}
	store := localmode.OpenStore(home)
	h := &devHooks{home: home, store: store, prof: prof}
	var found *localmode.Entry
	for i, e := range h.Entries() {
		if e.Name == "AGCTL_LLM_CORP_API_KEY" {
			found = &h.Entries()[i]
		}
	}
	if found == nil {
		t.Fatal("в «Доступах» нет AGCTL_LLM_CORP_API_KEY")
	}
	if found.Group != "llm" || !found.Secret || !found.Known {
		t.Fatalf("не тот entry: %+v", *found)
	}
	if !strings.Contains(found.Title, "corp-litellm") || !strings.Contains(found.Hint, "llm.corp.example") {
		t.Fatalf("подпись без провайдера/эндпоинта: %+v", *found)
	}
	if found.Set {
		t.Fatal("ключ ещё не задан, Set должен быть false")
	}
}
