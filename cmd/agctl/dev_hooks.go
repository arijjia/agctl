package main

// dev_hooks.go — server.LocalHooks для agctl dev serve: лаунчер в браузере
// управляет доступами (хранилище ОС + config.yaml) и образом рабочего места
// (каталог профиля, docker pull / сборка на машине). config.yaml перечитывается
// на каждую операцию: его параллельно может править CLI (agctl dev secrets).

import (
	"context"
	"fmt"
	"github.com/arijjia/agctl/internal/assets"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/cli"
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

// Entries — «Доступы» лаунчера: манифест/хранилище + личные ключи доп.
// провайдеров профиля (extra_providers с api_key_env): подписка «провайдер
// + ключ» — каждый разработчик вводит свой токен, значение не покидает
// хранилище ОС.
func (h *devHooks) Entries() []localmode.Entry {
	entries := localmode.Entries(h.cfg(), h.store)
	if h.prof == nil {
		return entries
	}
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.Name] = true
	}
	for _, p := range h.prof.LLM.ExtraProviders {
		env := strings.TrimSpace(p.APIKeyEnv)
		if env == "" || seen[env] {
			continue
		}
		seen[env] = true
		hint := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
		if models := strings.TrimSpace(p.Models); models != "" {
			hint += " · модели: " + models
		}
		_, setErr := h.store.Get(env)
		entries = append(entries, localmode.Entry{
			Name:   env,
			Title:  "Ключ " + strings.TrimSpace(p.Name) + " (личный)",
			Hint:   hint,
			Group:  "llm",
			Secret: true,
			Known:  true,
			Set:    setErr == nil,
		})
	}
	return entries
}

func (h *devHooks) SetValue(name, value string, plain bool) error {
	return localmode.SetValue(h.home, h.store, name, value, plain)
}

func (h *devHooks) DeleteValue(name string) error {
	return localmode.DeleteValue(h.home, h.store, name)
}

// ContainerEnv — env рабочего места без ключей моделей: кроме имён по
// соглашению (localmode.IsModelKeyEnv) отсекаются api_key_env провайдеров
// профиля с произвольными именами.
func (h *devHooks) ContainerEnv() []string {
	env, _ := localmode.ContainerEnv(h.cfg(), h.store)
	skip := map[string]bool{}
	if h.prof != nil {
		for _, p := range h.prof.LLM.ExtraProviders {
			skip[strings.TrimSpace(p.APIKeyEnv)] = true
		}
	}
	out := env[:0]
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); !skip[k] {
			out = append(out, kv)
		}
	}
	return out
}

// Providers — личные провайдеры моделей из настроек дома.
func (h *devHooks) Providers() []localmode.Provider { return h.cfg().Providers }

func (h *devHooks) SaveProvider(p localmode.Provider, key *string) error {
	return localmode.SaveProvider(h.home, h.store, p, key)
}

func (h *devHooks) DeleteProvider(name string) error {
	return localmode.DeleteProvider(h.home, h.store, name)
}

// ModelKey — ключ модели из хранилища секретов по имени.
func (h *devHooks) ModelKey(env string) string {
	v, _ := h.store.Get(env)
	return v
}

func (h *devHooks) LLMKey() string {
	v, _ := h.store.Get("AGCTL_LLM_API_KEY")
	return v
}

func (h *devHooks) StoreBackend() string { return h.store.Backend() }

func (h *devHooks) CurrentImage() string {
	cfg := h.cfg()
	return cli.FirstNonEmpty(cfg.Image, devDefaultImage(h.prof, cfg.Flavor))
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
		im := server.LocalImage{Ref: r, Flavor: devFlavorOf(&localmode.Config{}, h.prof, r)}
		if meta, err := dockerrt.ImageInfo(context.Background(), r); err == nil {
			im.Present = true
			devDescribeImage(&im, meta)
		} else if v := assets.DshVersionFromTag(devImageTag(r)); v != "" {
			im.DshVersion = v // соберётся на этой версии dsh
		}
		out = append(out, im)
	}
	return out
}

// devDescribeImage — что внутри образа по его меткам и не устарел ли он
// для этой сборки agctl (уровень API guest).
func devDescribeImage(im *server.LocalImage, meta dockerrt.ImageMeta) {
	l := meta.Labels
	im.DshVersion = l[assets.LabelDshVersion]
	im.GuestRevision = l[assets.LabelGuestRevision]
	im.Variant = l[assets.LabelVariant]
	im.Built = l[assets.LabelCreated]
	if im.Built == "" && len(meta.Created) >= 10 {
		im.Built = meta.Created[:10]
	}
	im.SizeMB = meta.Size / (1 << 20)
	api, _ := strconv.Atoi(l[assets.LabelGuestAPI])
	im.GuestAPI = api
	switch {
	case api == 0:
		im.Outdated = true
		im.Note = "образ собран до меток версий: что внутри — неизвестно; если пресеты не видны в чате — пересоберите (make img_build) или скачайте свежий"
	case api < assets.GuestAPI:
		im.Outdated = true
		im.Note = fmt.Sprintf("образ старее платформы (API guest %d < %d): пересоберите или скачайте свежий", api, assets.GuestAPI)
	}
}

// devImageTag — тег ссылки образа ("latest", если не указан).
func devImageTag(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[i+1:]
	}
	return "latest"
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

func (h *devHooks) SetWorkDir(dir string) (string, error) {
	return localmode.SetWorkDir(h.home, dir)
}

// PathsReport — отчёт agctl dev paths для страницы «Где что лежит».
func (h *devHooks) PathsReport() any {
	return devPathsReport(localmode.Home{ID: localmode.HomeID(h.home), Dir: h.home, Source: "запущенный дом"})
}
