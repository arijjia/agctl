package dockerrt

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// TestSweepVersionDriftLive — живая проверка свипа на реальном демоне: хвост
// прошлой сборки (чужой образ под agctl-меткой) удаляется, контейнер текущей
// сборки остаётся. Без docker или без образа agctl-dsh тест скипается.
func TestSweepVersionDriftLive(t *testing.T) {
	rt, err := New(Config{Image: "agctl-dsh:0.2.2", Version: "live-test"}, nil)
	if err != nil {
		t.Skipf("docker недоступен: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	current := rt.currentImageID(ctx)
	if current == "" {
		t.Skipf("образ %s не собран (make image) — свип не с чем сравнивать", rt.cfg.Image)
	}

	prefix := fmt.Sprintf("agctl-chat-livetest-%d-", os.Getpid())
	mk := func(name, image string) string {
		res, err := rt.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
			Name: name,
			Config: &container.Config{
				Image:  image,
				Labels: map[string]string{labelWorkspace: "livetest"},
			},
		})
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return res.ID
	}
	staleID := mk(prefix+"stale", "node:22-bookworm-slim") // прошлая «сборка»
	freshID := mk(prefix+"fresh", rt.cfg.Image)            // текущая сборка
	t.Cleanup(func() {
		_, _ = rt.cli.ContainerRemove(context.Background(), staleID, client.ContainerRemoveOptions{Force: true})
		_, _ = rt.cli.ContainerRemove(context.Background(), freshID, client.ContainerRemoveOptions{Force: true})
	})

	if n := rt.SweepVersionDrift(ctx, prefix); n != 1 {
		t.Fatalf("SweepVersionDrift = %d, ждали 1 (только stale-хвост)", n)
	}
	if _, err := rt.cli.ContainerInspect(ctx, staleID, client.ContainerInspectOptions{}); !isNotFound(err) {
		t.Fatalf("stale-контейнер пережил свип: inspect err = %v", err)
	}
	if _, err := rt.cli.ContainerInspect(ctx, freshID, client.ContainerInspectOptions{}); err != nil {
		t.Fatalf("свежий контейнер ошибочно удалён свипом: %v", err)
	}
}
