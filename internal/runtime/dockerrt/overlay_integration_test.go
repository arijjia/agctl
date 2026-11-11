package dockerrt

import (
	"context"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/moby/moby/client"
)

// TestEnsureImageBuildsProfileOverlay — живой Docker (AGCTL_DOCKER_IT=1):
// EnsureImage собирает надстройку профиля поверх уже собранного базового
// образа (AGCTL_DOCKER_IT_BASE, по умолчанию agctl-dsh:0.2.2) под
// уникальным тегом и проверяет, что слой надстройки на месте.
func TestEnsureImageBuildsProfileOverlay(t *testing.T) {
	if os.Getenv("AGCTL_DOCKER_IT") != "1" {
		t.Skip("живой Docker: AGCTL_DOCKER_IT=1")
	}
	base := os.Getenv("AGCTL_DOCKER_IT_BASE")
	if base == "" {
		base = "agctl-dsh:0.2.2"
	}
	tag := "agctl-dsh-it-" + time.Now().Format("150405") + ":test"
	overlay := fstest.MapFS{"Dockerfile": {Data: []byte("ARG BASE\nFROM ${BASE}\nRUN echo overlay > /opt/agctl-overlay-mark\n")}}
	rt, err := New(Config{Image: tag, BaseImage: base, Overlay: overlay}, nil)
	if err != nil {
		t.Skipf("docker: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if _, err := rt.cli.ImageInspect(ctx, base); err != nil {
		t.Skipf("нет базового %s (make img_build): %v", base, err)
	}
	if err := rt.EnsureImage(ctx); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}
	t.Cleanup(func() { _, _ = rt.cli.ImageRemove(context.Background(), tag, client.ImageRemoveOptions{}) })
	if _, err := rt.cli.ImageInspect(ctx, tag); err != nil {
		t.Fatalf("надстройка не собрана: %v", err)
	}
}
