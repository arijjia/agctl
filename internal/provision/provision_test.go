package provision

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/backlog"
)

func testProvider() Provider {
	return Provider{BaseURL: "https://litellm.example/v1", Model: "qwen-test", APIKey: "sk-test-token"}
}

func TestRefreshProviderWritesMultipleBaseURLAliases(t *testing.T) {
	home := t.TempDir()
	p := Provider{
		BaseURL:  "https://corp-a.example/v1",
		BaseURLs: []string{"https://corp-a.example/v1", "https://corp-b.example/v1"},
		Models:   []string{"qwen-fast", "qwen-deep"}, APIKey: "secret-test-only",
	}
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(filepath.Join(home, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(settings)
	for _, want := range []string{
		"    agctl:", "baseURL: \"https://corp-a.example/v1\"",
		"    agctl-2:", "baseURL: \"https://corp-b.example/v1\"",
		"provider: agctl", "model: \"qwen-fast\"", "id: \"qwen-deep\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings.yaml lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, p.APIKey) {
		t.Fatal("API key must not appear in settings.yaml")
	}
}

func TestRefreshProviderWritesNamedAndExtraProviders(t *testing.T) {
	home := t.TempDir()
	p := Provider{
		Name: "zai-coding-cn", BaseURL: "https://api.z.ai/api/coding/paas/v4",
		Model: "glm-5.3-flash", Models: []string{"glm-5.3", "glm-5.3-flash"},
		APIKey: "unit-test-zai",
		Extras: []ExtraProvider{{
			Name: "utmn-litellm", BaseURL: "https://litellm.example.invalid/v1",
			Model: "qwen-3.8-fast", Models: []string{"qwen-3.8-deep", "qwen-3.8-fast"},
			APIKeyEnv: "AGCTL_LLM_CORP_API_KEY", APIKey: "unit-test-corp",
		}},
	}
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	settings, err := os.ReadFile(filepath.Join(home, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(settings)
	for _, want := range []string{
		"    zai-coding-cn:", "baseURL: \"https://api.z.ai/api/coding/paas/v4\"",
		"    utmn-litellm:", "baseURL: \"https://litellm.example.invalid/v1\"",
		"apiKeyEnv: AGCTL_LLM_CORP_API_KEY", "id: \"qwen-3.8-deep\"",
		"provider: zai-coding-cn", "model: \"glm-5.3-flash\"",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings.yaml lacks %q:\n%s", want, text)
		}
	}
	for _, secret := range []string{"unit-test-zai", "unit-test-corp"} {
		if strings.Contains(text, secret) {
			t.Fatalf("API key %q must not appear in settings.yaml", secret)
		}
	}
	creds, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AGCTL_LLM_API_KEY: \"unit-test-zai\"", "AGCTL_LLM_CORP_API_KEY: \"unit-test-corp\""} {
		if !strings.Contains(string(creds), want) {
			t.Fatalf(".credentials.yaml lacks %q:\n%s", want, creds)
		}
	}
}

// TestProvisionFull — полный цикл: bare+клон+шаблон+dsh-home, autosave.
func TestProvisionFull(t *testing.T) {
	home := t.TempDir()
	l := Paths(home, "anna", "w-abcd1234ef56", "Моя Работа")
	p := testProvider()

	if err := Provision(l, p, "base", assets.RepoTemplate, assets.DshSkel); err != nil {
		t.Fatalf("provision: %v", err)
	}

	// bare: HEAD → main, есть коммит
	bare, err := git.PlainOpen(l.Repo)
	if err != nil {
		t.Fatalf("bare open: %v", err)
	}
	head, err := bare.Reference(plumbing.HEAD, true)
	if err != nil {
		t.Fatalf("bare HEAD resolve: %v", err)
	}
	if head.Name() != plumbing.NewBranchReferenceName("main") {
		t.Fatalf("bare HEAD = %s, ждали main", head.Name())
	}

	// рабочий клон: шаблон на месте, дерево чистое
	for _, f := range []string{"AGENTS.md", "README.md", "skills/backlog/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(l.Work, f)); err != nil {
			t.Errorf("нет файла шаблона %s: %v", f, err)
		}
	}
	repo, err := git.PlainOpen(l.Work)
	if err != nil {
		t.Fatalf("work open: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	st, err := wt.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.IsClean() {
		t.Fatalf("рабочее дерево не чистое после provision: %v", st)
	}

	// dsh-home: скелет + настройки провайдера + токен (0600)
	creds := filepath.Join(l.DshHome, ".credentials.yaml")
	b, err := os.ReadFile(creds)
	if err != nil {
		t.Fatalf("нет .credentials.yaml: %v", err)
	}
	if !strings.Contains(string(b), "sk-test-token") || !strings.Contains(string(b), "AGCTL_LLM_API_KEY") {
		t.Fatalf(".credentials.yaml без токена/ref: %s", b)
	}
	// На Windows нет POSIX-прав: файл всегда 0666, chmod 0600 не имеет эффекта.
	fi, err := os.Stat(creds)
	if err != nil {
		t.Fatalf("нет .credentials.yaml: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf(".credentials.yaml должен быть 0600, есть %v", fi.Mode().Perm())
	}
	settings, err := os.ReadFile(filepath.Join(l.DshHome, "settings.yaml"))
	if err != nil {
		t.Fatalf("нет settings.yaml: %v", err)
	}
	for _, want := range []string{"litellm.example/v1", "qwen-test", "apiKeyEnv: AGCTL_LLM_API_KEY"} {
		if !strings.Contains(string(settings), want) {
			t.Errorf("settings.yaml без %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(l.DshHome, "profiles", "web", "package.json")); err != nil {
		t.Errorf("нет скелета профиля web: %v", err)
	}

	// autosave: новое изменение → commit+push в bare
	if err := os.WriteFile(filepath.Join(l.Work, "note.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := AutosaveOnce(l.Work)
	if err != nil {
		t.Fatalf("autosave: %v", err)
	}
	if !changed {
		t.Fatal("autosave не закоммитил изменение")
	}
	changed, err = AutosaveOnce(l.Work)
	if err != nil || changed {
		t.Fatalf("повторный autosave должен быть no-op: changed=%v err=%v", changed, err)
	}
	// в bare появился второй коммит
	mainRef, err := bare.Reference(plumbing.NewBranchReferenceName("main"), true)
	if err != nil {
		t.Fatal(err)
	}
	iter, err := bare.Log(&git.LogOptions{From: mainRef.Hash()})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	_ = iter.ForEach(func(*object.Commit) error { n++; return nil })
	if n < 2 {
		t.Fatalf("в bare %d коммитов, ждали ≥2", n)
	}
}

// TestBacklogFlow — CLI поверх свежего репо: check/new/done.
func TestBacklogFlow(t *testing.T) {
	home := t.TempDir()
	l := Paths(home, "anna", "w-abcd1234ef56", "ws")
	if err := Provision(l, testProvider(), "base", assets.RepoTemplate, assets.DshSkel); err != nil {
		t.Fatalf("provision: %v", err)
	}
	root := l.Work
	// --root — сам каталог backlog/ (как возвращает Root()); cwd-поиск в CLI тот же.
	backlogDir := filepath.Join(root, "backlog")
	run := func(args ...string) int {
		return backlog.Main(append([]string{"--root", backlogDir}, args...), os.Stdout, os.Stderr)
	}
	if code := run("check"); code != 0 {
		t.Fatalf("backlog check = %d, ждали 0", code)
	}
	if code := run("new", "--title", "Починить велосипед", "--priority", "P1"); code != 0 {
		t.Fatalf("backlog new = %d", code)
	}
	// карточка появилась
	entries, _ := os.ReadDir(filepath.Join(root, "backlog", "open"))
	if len(entries) != 2 {
		t.Fatalf("в open %d карточек, ждали 2", len(entries))
	}
	// done onboarding
	if code := run("done", "onboarding"); code != 0 {
		t.Fatalf("backlog done = %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "backlog", "done", "2026-09-28-onboarding.md")); err != nil {
		t.Fatalf("карточка не переехала в done/: %v", err)
	}
	if code := run("check"); code != 0 {
		t.Fatalf("check после done = %d", code)
	}
}

// object нужен только для подсчёта коммитов в тесте.

func TestRefreshProviderEmptyKeyOmitsRef(t *testing.T) {
	home := t.TempDir()
	if err := RefreshProvider(home, Provider{}); err != nil {
		t.Fatal(err)
	}
	creds, err := os.ReadFile(filepath.Join(home, ".credentials.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(creds), "AGCTL_LLM_API_KEY") {
		t.Fatalf("пустой ключ не должен попадать в credentials: %s", creds)
	}
}

func TestRefreshProviderRemovesStalePatchAliases(t *testing.T) {
	home := t.TempDir()
	patchDir := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(patchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	patch := `- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: agctl
    model: old-model
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      agctl:
        apiKeyEnv: AGCTL_LLM_API_KEY
        api: openai-completions
        baseURL: https://old.example/v1
        models:
          - id: old-model
            name: old-model
- id: keep-me
  name: "@example/keep-me"
  config:
    value: keep
`
	if err := os.WriteFile(filepath.Join(patchDir, "cordis.patch.yml"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{
		Name: "zai-coding-cn", BaseURL: "https://zai.example/v1",
		Model: "glm-fast", Models: []string{"glm-fast", "glm-deep"}, APIKey: "primary-key",
		Extras: []ExtraProvider{{
			Name: "corp-litellm", BaseURL: "https://litellm.example/v1",
			Model: "qwen-fast", Models: []string{"qwen-fast", "qwen-deep"},
			APIKeyEnv: "AGCTL_LLM_CORP_API_KEY", APIKey: "corp-key",
		}},
	}
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(patchDir, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	foundKeep, foundLLM, foundDefault := false, false, false
	for _, entry := range entries {
		switch entry["name"] {
		case "@example/keep-me":
			foundKeep = true
		case "@deepseek-ai/dsh-llm-pi-ai":
			foundLLM = true
			cfg, _ := entry["config"].(map[string]any)
			providers, _ := cfg["providers"].(map[string]any)
			if _, ok := providers["agctl"]; ok {
				t.Fatalf("stale provider alias survived: %#v", providers)
			}
			if _, ok := providers["zai-coding-cn"]; !ok {
				t.Fatalf("zai-coding-cn missing from patch: %#v", providers)
			}
			if _, ok := providers["corp-litellm"]; !ok {
				t.Fatalf("corp-litellm missing from patch: %#v", providers)
			}
		case "@deepseek-ai/dsh-agent-default-model":
			foundDefault = true
			cfg, _ := entry["config"].(map[string]any)
			if cfg["provider"] != "zai-coding-cn" || cfg["model"] != "glm-fast" {
				t.Fatalf("default model config = %#v", cfg)
			}
		}
	}
	if !foundKeep || !foundLLM || !foundDefault {
		t.Fatalf("patch entries lost: keep=%v llm=%v default=%v", foundKeep, foundLLM, foundDefault)
	}
}

func TestRefreshProviderThenEnsureChatPluginsKeepsSingleAccountDisable(t *testing.T) {
	home := t.TempDir()
	profile := filepath.Join(home, "profiles", "web")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "package.json"), []byte(`{"dsh":{"profile":{"bundles":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: agctl
    model: old-model
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      agctl:
        apiKeyEnv: AGCTL_LLM_API_KEY
        api: openai-completions
        baseURL: https://old.example/v1
        models:
          - id: old-model
            name: old-model
- id: ui-settings-account
  name: "@deepseek-ai/dsh-client-ui-settings-account"
  disabled: true
`
	if err := os.WriteFile(filepath.Join(profile, "cordis.patch.yml"), []byte(patch), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Provider{Name: "zai-coding-cn", BaseURL: "https://zai.example/v1", Model: "glm-fast", Models: []string{"glm-fast"}, APIKey: "primary-key"}
	if err := RefreshProvider(home, p); err != nil {
		t.Fatal(err)
	}
	if err := EnsureChatPlugins(home, assets.DshSkel); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(profile, "cordis.patch.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "@deepseek-ai/dsh-client-ui-settings-account"); got != 1 {
		t.Fatalf("account launcher entries = %d, want 1:\n%s", got, raw)
	}
	if strings.Contains(string(raw), "baseURL: https://old.example/v1") {
		t.Fatalf("stale provider survived:\n%s", raw)
	}
}
