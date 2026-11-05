package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

func TestProfExtraProvidersResolvesKeysFromEnv(t *testing.T) {
	dir := t.TempDir()
	body := `schema: 1
id: providers-test
brand:
  name: Providers Test
llm:
  provider_name: zai-coding-cn
  default_base_url: https://zai.example/v1
  default_model: glm-fast
  default_models: glm-fast,glm-deep
  extra_providers:
    - name: corp-litellm
      base_url: https://litellm.example/v1
      model: qwen-fast
      models: qwen-fast,qwen-deep
      api_key_env: AGCTL_LLM_CORP_API_KEY
`
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	prof, err := profile.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGCTL_LLM_CORP_API_KEY", "unit-test-corp-key")
	extras := profExtraProviders(prof)
	if len(extras) != 1 {
		t.Fatalf("extras = %d, want 1", len(extras))
	}
	got := extras[0]
	if got.Name != "corp-litellm" || got.BaseURL != "https://litellm.example/v1" || got.Model != "qwen-fast" {
		t.Fatalf("extra provider = %+v", got)
	}
	if got.APIKeyEnv != "AGCTL_LLM_CORP_API_KEY" || got.APIKey != "unit-test-corp-key" {
		t.Fatalf("extra provider key resolution = %+v", got)
	}
	if name := llmProviderName(prof); name != "zai-coding-cn" {
		t.Fatalf("provider name = %q", name)
	}
	t.Setenv("AGCTL_LLM_PROVIDER", "env-provider")
	if name := llmProviderName(prof); name != "env-provider" {
		t.Fatalf("env provider name = %q", name)
	}
}
