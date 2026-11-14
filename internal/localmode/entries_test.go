package localmode

import (
	"strings"
	"testing"
)

func TestEntriesSetDelete(t *testing.T) {
	t.Setenv("AGCTL_DEV_SECRETS", "file")
	home := t.TempDir()
	if err := (&Config{}).Save(home); err != nil {
		t.Fatal(err)
	}
	store := OpenStore(home)
	if err := SetValue(home, store, "GITLAB_TOKEN", "glpat-1", false); err != nil {
		t.Fatal(err)
	}
	if err := SetValue(home, store, "GITLAB_URL", "https://git.example.org", false); err != nil { // манифест: несекретное
		t.Fatal(err)
	}
	if err := SetValue(home, store, "MY_FLAG", "on", true); err != nil {
		t.Fatal(err)
	}
	if err := SetValue(home, store, "bad name", "x", false); err == nil {
		t.Fatal("невалидное имя принято")
	}
	if err := SetValue(home, store, "X_EMPTY", "  ", false); err == nil {
		t.Fatal("пустое значение принято")
	}
	cfg, _ := Load(home)
	if cfg.Env["GITLAB_URL"] == "" || cfg.Env["MY_FLAG"] != "on" {
		t.Fatalf("несекретное не в dev.yaml: %v", cfg.Env)
	}
	if cfg.Env["GITLAB_TOKEN"] != "" {
		t.Fatal("секрет попал в dev.yaml")
	}
	by := map[string]Entry{}
	for _, e := range Entries(cfg, store) {
		by[e.Name] = e
	}
	if !by["GITLAB_TOKEN"].Set || by["GITLAB_TOKEN"].Value != "" || !by["GITLAB_TOKEN"].Secret {
		t.Fatalf("секрет: %+v", by["GITLAB_TOKEN"])
	}
	if by["GITLAB_URL"].Value != "https://git.example.org" || by["MY_FLAG"].Secret {
		t.Fatalf("несекретные: %+v %+v", by["GITLAB_URL"], by["MY_FLAG"])
	}
	if by["JIRA_API_TOKEN"].Set || !by["JIRA_API_TOKEN"].Known {
		t.Fatalf("незаданный из манифеста: %+v", by["JIRA_API_TOKEN"])
	}
	env, _ := ContainerEnv(cfg, store)
	if !strings.Contains(strings.Join(env, ","), "GITLAB_TOKEN=glpat-1") {
		t.Fatalf("env: %v", env)
	}
	if err := DeleteValue(home, store, "GITLAB_TOKEN"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(home)
	for _, e := range Entries(cfg, store) {
		if e.Name == "GITLAB_TOKEN" && e.Set {
			t.Fatal("не удалён")
		}
	}
	if err := SetImage(home, "img-1c:1", "1c"); err != nil {
		t.Fatal(err)
	}
	cfg, _ = Load(home)
	if cfg.Image != "img-1c:1" || cfg.Flavor != "1c" {
		t.Fatalf("образ: %+v", cfg)
	}
}
