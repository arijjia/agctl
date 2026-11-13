package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentCredentialsFiles(t *testing.T) {
	env := newTestEnv(t, 1)
	s := env.srv
	extra := s.writeAgentCredentials(7, []string{
		"GITLAB_URL=https://gitlab.example.org", "GITLAB_TOKEN=glpat-x'y",
		"JIRA_URL=https://jira.example.org", "AGCTL_BRIDGE_URL=http://host.docker.internal:8930", "AGCTL_BRIDGE_TOKEN=br",
	})
	joined := strings.Join(extra, "\n")
	for _, want := range []string{"AGCTL_CREDENTIALS_ENV=/home/dsh/.agctl/credentials.env", "GIT_CONFIG_GLOBAL=/home/dsh/.agctl/gitconfig", "GLAB_CONFIG_DIR=/home/dsh/.agctl/glab"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("нет %s в %v", want, extra)
		}
	}
	for _, name := range []string{"TOKEN", "KEY", "SECRET", "PASSWORD"} {
		for _, kv := range extra {
			k, _, _ := strings.Cut(kv, "=")
			if strings.Contains(k, name) {
				t.Fatalf("имя %s вычистит dsh — указатели на файлы должны его переживать", k)
			}
		}
	}
	dir := filepath.Join(s.projectDshHome(7), ".agctl")
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info, _ := os.Stat(filepath.Join(dir, name)); os.PathSeparator == '/' && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: права %v", name, info.Mode().Perm())
		}
		return string(raw)
	}
	if got := read("credentials.env"); !strings.Contains(got, `GITLAB_TOKEN='glpat-x'\''y'`) {
		t.Fatalf("credentials.env: %s", got)
	}
	if got := read("git-credentials"); got != "https://oauth2:glpat-x%27y@gitlab.example.org\n" {
		t.Fatalf("git-credentials: %q", got)
	}
	if got := read("gitconfig"); !strings.Contains(got, "store --file=/home/dsh/.agctl/git-credentials") {
		t.Fatalf("gitconfig: %s", got)
	}
	if got := read(filepath.Join("glab", "config.yml")); !strings.Contains(got, "gitlab.example.org:") || !strings.Contains(got, `token: "glpat-x'y"`) {
		t.Fatalf("glab: %s", got)
	}
	if got := read("bridge.json"); !strings.Contains(got, `"token":"br"`) {
		t.Fatalf("bridge.json: %s", got)
	}
	skill, _ := os.ReadFile(filepath.Join(s.projectDshHome(7), "skills", "agctl-workspace-access", "SKILL.md"))
	if strings.Contains(string(skill), "glpat") || !strings.Contains(string(skill), "`GITLAB_TOKEN`") {
		t.Fatalf("скилл называет доступы, но не содержит значений: %s", skill)
	}
	// доступ отозван — файлы убираются
	if extra := s.writeAgentCredentials(7, nil); extra != nil {
		t.Fatal("без env — без указателей")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("каталог доступов удаляется при отзыве")
	}
}
