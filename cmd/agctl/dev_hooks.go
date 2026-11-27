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
	"sync"

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
	entries := localmode.Entries(h.cfg(), h.store, h.profileEnv())
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

// profileEnv — несекретные значения профиля по умолчанию (env профиля).
func (h *devHooks) profileEnv() map[string]string {
	if h.prof == nil {
		return nil
	}
	return h.prof.View().Env
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
	env, _ := localmode.ContainerEnv(h.cfg(), h.store, h.profileEnv())
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

// devPlanBuild — что и из чего собирается для образа ref (план и файлы
// контекста в разделе «Образы»): ошибка — образ не из семейства agctl-dsh
// этого профиля. guest-бинарь — под архитектуру демона Docker.
func (h *devHooks) devPlanBuild(ref string) (dockerrt.DevBuild, error) {
	local := dockerrt.LocalImageName(ref)
	b := devBuildSpec(h.prof, local, devFlavorOf(&localmode.Config{}, h.prof, ref))
	if plan := dockerrt.DevBuildPlan(b); plan[len(plan)-1] != local {
		return b, fmt.Errorf("образ %s не из семейства agctl-dsh этого профиля (сборка дала бы %s) — его можно только скачать (docker pull)", ref, plan[len(plan)-1])
	}
	b.Arch = devDaemonArch()
	return b, nil
}

// devArch — архитектура демона Docker, узнанная один раз за процесс (на
// ней и решается, какой guest-бинарь войдёт в образ).
var devArch struct {
	sync.Mutex
	v string
}

func devDaemonArch() string {
	devArch.Lock()
	defer devArch.Unlock()
	if devArch.v == "" {
		devArch.v = dockerrt.DaemonArch(context.Background())
	}
	return devArch.v
}

// ImagePlan — как образ ref попадает на машину: docker pull, при «нет
// такого» — ступени сборки с Dockerfile, аргументами, контекстом и тем, что
// качается из сети (раздел «Образы» лаунчера).
func (h *devHooks) ImagePlan(ref string) (server.LocalImagePlan, error) {
	b, err := h.devPlanBuild(ref)
	if err != nil {
		return server.LocalImagePlan{}, err
	}
	local := b.Target
	stages, err := dockerrt.DevBuildStages(b)
	if err != nil {
		return server.LocalImagePlan{}, err
	}
	out := server.LocalImagePlan{Ref: ref}
	if local != ref {
		out.Pull = "docker pull " + ref + "; реестр ответил «нет такого» или пакет закрыт — сборка ступеней ниже как " + local + " с тегом " + ref + "; при сбое связи — ошибка, без сборки"
	} else {
		out.Pull = "docker pull " + ref + " (локальное имя: в Docker Hub его обычно нет) — затем сборка ступеней ниже; готовые ступени не пересобираются"
	}
	for _, st := range stages {
		var ctxItems []server.LocalImageContext
		for _, c := range st.Context {
			ctxItems = append(ctxItems, server.LocalImageContext{
				Name: c.Name, Dir: c.Dir, Origin: c.Origin, From: c.From, Path: c.Path, Size: c.Size,
				Files: c.Files, Missing: c.Missing, Modified: c.Modified, Note: c.Note,
			})
		}
		out.Stages = append(out.Stages, server.LocalImageStage{
			Ref: st.Ref, Kind: st.Kind, Title: st.Title, Source: st.Source, Dockerfile: st.Dockerfile,
			Args: st.Args, Context: ctxItems, From: st.From, Apt: st.Apt, Npm: st.Npm, Downloads: st.Downloads,
		})
	}
	return out, nil
}

// ImageFile — файл или каталог контекста сборки ступени stage (с 1) образа
// ref: встроенные в бинарь файлы, скелет dsh-home, guest-бинарь на диске
// (двоичный — ошибка с размером), файлы профиля.
func (h *devHooks) ImageFile(ref string, stage int, rel string) (server.LocalImageFile, error) {
	b, err := h.devPlanBuild(ref)
	if err != nil {
		return server.LocalImageFile{}, err
	}
	f, err := dockerrt.DevContextFile(b, stage, rel)
	if err != nil {
		return server.LocalImageFile{}, err
	}
	out := server.LocalImageFile{Path: f.Path, Size: f.Size, Dir: f.Dir, Text: f.Text}
	for _, e := range f.Files {
		out.Files = append(out.Files, server.LocalImageFileItem{Path: e.Path, Size: e.Size})
	}
	return out, nil
}

// RemoveImage — удалить тег образа из Docker этой машины.
func (h *devHooks) RemoveImage(ctx context.Context, ref string) error {
	return dockerrt.RemoveImage(ctx, ref)
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
