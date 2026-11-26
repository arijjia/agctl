package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
)

// agctl dev images --json: каталог образов профиля и образ основного места;
// без Docker образы просто «нет на машине», команда не падает.
func TestDevImagesJSON(t *testing.T) {
	layoutRoot(t)
	t.Setenv("DOCKER_HOST", "unix:///nonexistent/docker.sock") // без демона: всё «нет»
	h, _ := localmode.SelectHome("")
	if err := devInit(h, []string{"--yes", "--profile", "agctl", "--workdir", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	home := localmode.HomeDir("agctl")
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	err := devImages(home, []string{"--json"})
	w.Close()
	os.Stdout = old
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(r)
	var got struct {
		Current string `json:"current"`
		Images  []struct {
			Ref     string `json:"ref"`
			Present bool   `json:"present"`
			Flavor  string `json:"flavor"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	refs := []string{}
	for _, im := range got.Images {
		refs = append(refs, im.Ref)
		if im.Present {
			t.Errorf("%s «есть на машине» без Docker", im.Ref)
		}
		if strings.HasPrefix(im.Ref, "agctl-dsh-min:") && im.Flavor != "min" {
			t.Errorf("минимальный образ без вкуса min: %+v", im)
		}
	}
	joined := strings.Join(refs, ",")
	if got.Current != "agctl-dsh:0.2.2" || !strings.Contains(joined, "agctl-dsh-min:0.2.2") || strings.Contains(joined, "nossh") {
		t.Fatalf("каталог: current=%s images=%s", got.Current, joined)
	}
}
