package localmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Доступы из окружения хоста: переносятся только в пустые записи (секреты —
// в хранилище, адреса и логины — в config.yaml), синонимы и env_from
// профиля работают, заданное в agctl не перезаписывается, удалённое
// берётся снова, env_import: false выключает перенос.
func TestImportHostEnv(t *testing.T) {
	t.Setenv("AGCTL_DEV_SECRETS", "file")
	home := t.TempDir()
	if err := (&Config{}).Save(home); err != nil {
		t.Fatal(err)
	}
	store := OpenStore(home)
	hostEnv := map[string]string{
		"LLM_TOKEN":                 "sk-corp",
		"AGCTL_LLM_API_KEY":         "zai-personal",
		"JIRA_USERNAME":             "dev",
		"JIRA_URL":                  "https://jira.example.org",
		"CONFLUENCE_PERSONAL_TOKEN": "conf-1",
		"TAVILY_API_KEY":            "tvly-1",
		"UNRELATED_TOKEN":           "never",
	}
	getenv := func(k string) string { return hostEnv[k] }
	cfg, _ := Load(home)
	sources := HostEnvSources(cfg, map[string][]string{"AGCTL_LLM_API_KEY": {"LLM_TOKEN"}}, "TAVILY_API_KEY")
	if got := strings.Join(sources["AGCTL_LLM_API_KEY"], ","); got != "LLM_TOKEN,AGCTL_LLM_API_KEY" {
		t.Fatalf("env_from профиля — первым: %s", got)
	}
	got, err := ImportHostEnv(home, store, sources, getenv)
	if err != nil {
		t.Fatal(err)
	}
	moved := map[string]string{}
	for _, g := range got {
		moved[g.Name] = g.From
	}
	want := map[string]string{"AGCTL_LLM_API_KEY": "LLM_TOKEN", "JIRA_USER": "JIRA_USERNAME", "JIRA_URL": "JIRA_URL",
		"CONFLUENCE_TOKEN": "CONFLUENCE_PERSONAL_TOKEN", "TAVILY_API_KEY": "TAVILY_API_KEY"}
	for k, v := range want {
		if moved[k] != v {
			t.Fatalf("%s ← %q, ждали %q (всё: %v)", k, moved[k], v, moved)
		}
	}
	if _, ok := moved["UNRELATED_TOKEN"]; ok || len(moved) != len(want) {
		t.Fatalf("лишнее из окружения: %v", moved)
	}
	if v, _ := store.Get("AGCTL_LLM_API_KEY"); v != "sk-corp" {
		t.Fatalf("ключ модели в хранилище: %q", v)
	}
	cfg, _ = Load(home)
	if cfg.Env["JIRA_USER"] != "dev" || cfg.Env["JIRA_URL"] != "https://jira.example.org" || cfg.Env["CONFLUENCE_TOKEN"] != "" {
		t.Fatalf("config.yaml: %v", cfg.Env)
	}
	raw, _ := os.ReadFile(filepath.Join(home, "config.yaml"))
	for _, secret := range []string{"sk-corp", "conf-1", "tvly-1"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("секрет %q в config.yaml", secret)
		}
	}

	// заданное в agctl не перезаписывается
	hostEnv["LLM_TOKEN"], hostEnv["JIRA_USERNAME"] = "sk-other", "other"
	if got, err := ImportHostEnv(home, store, sources, getenv); err != nil || len(got) != 0 {
		t.Fatalf("повтор: %v %v", got, err)
	}
	if v, _ := store.Get("AGCTL_LLM_API_KEY"); v != "sk-corp" {
		t.Fatalf("ключ перезаписан: %q", v)
	}
	// удалённое — снова из окружения
	if err := DeleteValue(home, store, "AGCTL_LLM_API_KEY"); err != nil {
		t.Fatal(err)
	}
	if got, err := ImportHostEnv(home, store, sources, getenv); err != nil || len(got) != 1 || got[0].From != "LLM_TOKEN" {
		t.Fatalf("после удаления: %v %v", got, err)
	}
	if v, _ := store.Get("AGCTL_LLM_API_KEY"); v != "sk-other" {
		t.Fatalf("после удаления: %q", v)
	}
	// env_import: false
	cfg, _ = Load(home)
	off := false
	cfg.EnvImport = &off
	if err := cfg.Save(home); err != nil {
		t.Fatal(err)
	}
	_ = DeleteValue(home, store, "TAVILY_API_KEY")
	if got, err := ImportHostEnv(home, store, sources, getenv); err != nil || len(got) != 0 {
		t.Fatalf("env_import: false: %v %v", got, err)
	}
	if cfg, _ := Load(home); cfg.EnvImportOn() {
		t.Fatal("env_import: false не сохранился")
	}
}

// Вариант профиля во вложенном каталоге (org/dev с id org-dev) находится
// по id и виден в списке профилей.
func TestResolveProfileVariant(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AGCTL_ROOT", root)
	write := func(rel, body string) {
		p := filepath.Join(root, "profiles", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("org/profile.yaml", "schema: 1\nid: org\nbrand: {name: Орг}\nllm: {default_model: big}\n")
	write("org/dev/profile.yaml", "schema: 1\nid: org-dev\nextends: org\nllm: {default_model: corp}\n")
	p, ref, err := ResolveProfile(&Config{Profile: "org-dev"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "org-dev" || p.Brand.Name != "Орг" || p.LLM.DefaultModel != "corp" || !strings.HasSuffix(ref.Dir, filepath.Join("org", "dev")) {
		t.Fatalf("вариант: %+v %+v", p, ref)
	}
	if got := strings.Join(DiskProfiles(), ","); got != "org,org-dev" {
		t.Fatalf("список профилей: %s", got)
	}
}
