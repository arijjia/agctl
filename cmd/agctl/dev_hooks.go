package main

// dev_hooks.go — server.LocalHooks для agctl dev serve: лаунчер в браузере
// управляет доступами (хранилище ОС + dev.yaml) и образом рабочего места
// (каталог профиля, docker pull / сборка на машине). dev.yaml перечитывается
// на каждую операцию: его параллельно может править CLI (agctl dev secrets).

import (
	"context"
	"strings"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/server"
)

type devHooks struct {
	home  string
	store *localmode.Store
	prof  *profile.Data
}

func (h *devHooks) cfg() *localmode.Config {
	cfg, err := localmode.Load(h.home)
	if err != nil {
		return &localmode.Config{}
	}
	return cfg
}

func (h *devHooks) Entries() []localmode.Entry { return localmode.Entries(h.cfg(), h.store) }

func (h *devHooks) SetValue(name, value string, plain bool) error {
	return localmode.SetValue(h.home, h.store, name, value, plain)
}

func (h *devHooks) DeleteValue(name string) error {
	return localmode.DeleteValue(h.home, h.store, name)
}

func (h *devHooks) ContainerEnv() []string {
	env, _ := localmode.ContainerEnv(h.cfg(), h.store)
	return env
}

func (h *devHooks) LLMKey() string {
	v, _ := h.store.Get("AGCTL_LLM_API_KEY")
	return v
}

func (h *devHooks) StoreBackend() string { return h.store.Backend() }

func (h *devHooks) CurrentImage() string {
	cfg := h.cfg()
	return firstNonEmpty(cfg.Image, devDefaultImage(h.prof, cfg.Flavor))
}

// Images — каталог профиля (images.dev) + текущий образ, если он не оттуда;
// варианты «без SSH» скрыты: в локальном режиме SSH не нужен.
func (h *devHooks) Images() []server.LocalImage {
	var refs []string
	seen := map[string]bool{}
	add := func(r string) {
		if r != "" && !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	for _, r := range h.prof.Images.Dev {
		if !strings.Contains(r, "nossh") {
			add(r)
		}
	}
	if len(refs) == 0 {
		add(devDefaultImage(h.prof, ""))
	}
	add(h.CurrentImage())
	out := make([]server.LocalImage, 0, len(refs))
	for _, r := range refs {
		_, err := dockerrt.ImageID(context.Background(), r)
		out = append(out, server.LocalImage{Ref: r, Present: err == nil, Flavor: devFlavorOf(&localmode.Config{}, h.prof, r)})
	}
	return out
}

func (h *devHooks) SetImage(ref string) error {
	return localmode.SetImage(h.home, ref, devFlavorOf(&localmode.Config{}, h.prof, ref))
}

func (h *devHooks) EnsureImage(ctx context.Context, ref string, logf func(string)) error {
	if _, err := dockerrt.ImageID(ctx, ref); err == nil {
		return nil
	}
	return devPrepareImage(ctx, h.prof, ref, devFlavorOf(&localmode.Config{}, h.prof, ref), logf)
}
