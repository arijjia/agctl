package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/profile"
)

// devTestProfile — дисковый профиль org с images.registry = registry.
func devTestProfile(t *testing.T, registry string) *profile.Data {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "org")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema: 1\nid: org\nbrand:\n  name: Org\nimages:\n  dev: [agctl-dsh:0.2.2, agctl-dsh-web:0.2.2]\n"
	if registry != "" {
		body += "  registry: " + registry + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "profile.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := profile.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Откуда тянется образ: реестр ядра по умолчанию, свой реестр профиля,
// "none" — только сборка (pull — понятная ошибка без Docker); ссылки с
// реестром — как есть. То же — в плане «Как собирается» и в каталоге.
func TestDevImagesRegistry(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock") // без демона
	def := devTestProfile(t, "")
	own := devTestProfile(t, "registry.example.org:5000/team")
	none := devTestProfile(t, "none")
	cases := []struct {
		p         *profile.Data
		ref, want string
	}{
		{def, "agctl-dsh:0.2.2", "ghcr.io/arijjia/agctl-dsh:0.2.2"},
		{own, "agctl-dsh-web:0.2.2", "registry.example.org:5000/team/agctl-dsh-web:0.2.2"},
		{none, "agctl-dsh:0.2.2", ""},
		{none, "ghcr.io/org/agctl-dsh:1", "ghcr.io/org/agctl-dsh:1"},
		{def, "ghcr.io/org/agctl-dsh:1", "ghcr.io/org/agctl-dsh:1"},
		{nil, "agctl-dsh-min:0.2.2", "ghcr.io/arijjia/agctl-dsh-min:0.2.2"},
	}
	for _, c := range cases {
		if got := devPullFrom(c.p, c.ref); got != c.want {
			t.Errorf("devPullFrom(%s) = %q, ждали %q", c.ref, got, c.want)
		}
	}

	h := &devHooks{home: t.TempDir(), prof: own}
	plan, err := h.ImagePlan("agctl-dsh-web:0.2.2")
	if err != nil {
		t.Fatal(err)
	}
	if plan.PullRef != "registry.example.org:5000/team/agctl-dsh-web:0.2.2" || !strings.Contains(plan.Pull, "docker pull "+plan.PullRef+" (images.registry профиля)") ||
		!strings.Contains(plan.Pull, "сначала тоже из registry.example.org:5000/team") {
		t.Fatalf("план, свой реестр: %+v", plan.Pull)
	}
	if plan, err := (&devHooks{home: t.TempDir(), prof: def}).ImagePlan("agctl-dsh:0.2.2"); err != nil ||
		!strings.Contains(plan.Pull, "по умолчанию") || plan.PullRef != "ghcr.io/arijjia/agctl-dsh:0.2.2" {
		t.Fatalf("план, реестр по умолчанию: %+v %v", plan.Pull, err)
	}
	hn := &devHooks{home: t.TempDir(), prof: none}
	if plan, err := hn.ImagePlan("agctl-dsh:0.2.2"); err != nil || plan.PullRef != "" || !strings.Contains(plan.Pull, "images.registry: none") {
		t.Fatalf("план, реестр выключен: %+v %v", plan, err)
	}
	for _, im := range h.Images() {
		if im.PullFrom != "registry.example.org:5000/team/"+im.Ref {
			t.Errorf("каталог: %s pullFrom=%q", im.Ref, im.PullFrom)
		}
	}
	if _, err := hn.PullImage(context.Background(), "agctl-dsh:0.2.2", func(string) {}); !errors.Is(err, errRegistryOff) {
		t.Fatalf("pull при images.registry: none: %v", err)
	}
}

// Почему не скачался: «нет такого или закрыт» отдельно от сбоя связи; без
// повтора «pull <ref>:».
func TestDevPullReason(t *testing.T) {
	from := "ghcr.io/arijjia/agctl-dsh-web:0.2.2"
	got := devPullReason(errors.New("pull "+from+": Error response from daemon: denied"), from)
	if !strings.HasPrefix(got, "в реестре нет такого образа") || strings.Contains(got, "pull "+from) || !strings.Contains(got, "denied") {
		t.Fatalf("нет образа: %s", got)
	}
	got = devPullReason(errors.New(`pull `+from+`: Get "https://ghcr.io/v2/": dial tcp: lookup ghcr.io: no such host`), from)
	if !strings.HasPrefix(got, "реестр недоступен") {
		t.Fatalf("сбой связи: %s", got)
	}
}
