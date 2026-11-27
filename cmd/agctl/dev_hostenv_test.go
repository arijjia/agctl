package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
)

// Источники доступов из окружения: env_from профиля первым, ключи доп.
// провайдеров и переменные MCP-пакетов каталога профиля — под своими именами.
func TestDevHostEnvSources(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"profile.yaml":              "schema: 1\nid: p\nbrand: {name: P}\nllm:\n  extra_providers:\n    - {name: or, base_url: https://or.example/v1, model: m, models: m, api_key_env: OR_API_KEY}\nenv_from:\n  AGCTL_LLM_API_KEY: [LLM_TOKEN]\n",
		"catalog/exa/manifest.json": `{"id":"exa","title":"Exa","mcp":{"server_name":"exa","command":"npx","args":["-y","exa-mcp-server"],"env":["EXA_API_KEY"]}}`,
	}
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	prof, err := profile.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	src := devHostEnvSources(&localmode.Config{}, prof)
	if got := strings.Join(src["AGCTL_LLM_API_KEY"], ","); got != "LLM_TOKEN,AGCTL_LLM_API_KEY" {
		t.Fatalf("ключ модели: %s", got)
	}
	for _, name := range []string{"OR_API_KEY", "EXA_API_KEY", "TAVILY_API_KEY", "JIRA_API_TOKEN"} {
		if len(src[name]) == 0 {
			t.Fatalf("нет источника для %s: %v", name, src)
		}
	}
}
