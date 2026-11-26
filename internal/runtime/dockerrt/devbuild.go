package dockerrt

// devbuild.go — сборка образа рабочего места ЛОКАЛЬНОГО режима на машине
// разработчика без репозитория (agctl dev up, когда образа нет ни локально,
// ни в реестре). Повторяет цепочку make img_build:
//
//	agctl-dsh:<tag>                       базовый (встроенный контекст)
//	agctl-dsh-min:<tag>                   минимальный (Dockerfile.min, без ступеней сверху)
//	agctl-dsh-web:<tag>                   вкус web (встроенный flavors/web)
//	agctl-dsh[-web]-<профиль>:<tag>       надстройка профиля (images.dockerfile)
//	agctl-dsh-<профиль>-<вкус>:<tag>      вкус профиля (image/flavors/<вкус>)
//
// Готовые ступени не пересобираются.

import (
	"context"
	"fmt"
	"github.com/moby/moby/client"
	"io/fs"
	"os"
	"path"
	"strings"
)

// DevBuild — что собрать.
type DevBuild struct {
	Target  string // целевой образ (явный выбор из config.yaml или образ профиля)
	Flavor  string // web | вкус профиля | ""
	BinDir  string // где искать linux-сборку agctl-guest (<корень>/bin/guest; плюс рядом с бинарём)
	ImageFS fs.FS  // встроенный контекст (префикс "image")
	SkelFS  fs.FS  // скелет DSH_HOME (префикс "dsh-skel")

	ProfileID         string
	Overlay           fs.FS  // контекст надстройки профиля (nil — нет)
	OverlayDockerfile string // имя Dockerfile в Overlay
	ProfileFS         fs.FS  // корень профиля (image/flavors/<вкус>/Dockerfile)

	// Сборка agctl — в метки образа (io.agctl.guest.*, created).
	Version, Commit, BuildDate string

	Log func(string)
}

// imageTag — тег ссылки ("0.2.2" из "agctl-dsh-org:0.2.2"; без тега — latest).
func imageTag(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[i+1:]
	}
	return "latest"
}

// withSuffix — "agctl-dsh:0.2.2" + "org" → "agctl-dsh-org:0.2.2"
// (те же имена, что make overlay_tag/flavor_tag и profile.OverlayImage).
func withSuffix(ref, suffix string) string {
	slash := strings.LastIndex(ref, "/")
	if i := strings.LastIndex(ref, ":"); i > slash {
		return ref[:i] + "-" + suffix + ref[i:]
	}
	return ref + "-" + suffix
}

// LocalImageName — имя образа без реестра и пространства имён:
// "ghcr.io/org/agctl-dsh-min:0.2.2" → "agctl-dsh-min:0.2.2". Чужие образы
// (не agctl-dsh*) возвращаются как есть.
func LocalImageName(ref string) string {
	slash := strings.LastIndex(ref, "/")
	if slash < 0 {
		return ref
	}
	name := ref[slash+1:]
	if !strings.HasPrefix(name, "agctl-dsh") {
		return ref
	}
	return name
}

// DevBuildPlan — ступени сборки (для сообщений и тестов): итоговый образ —
// последний.
func DevBuildPlan(b DevBuild) []string {
	tag := imageTag(b.Target)
	if b.Flavor == "min" {
		return []string{"agctl-dsh-min:" + tag}
	}
	cur := "agctl-dsh:" + tag
	plan := []string{cur}
	if b.Flavor == "web" {
		cur = "agctl-dsh-web:" + tag
		plan = append(plan, cur)
	}
	if b.Overlay != nil && b.ProfileID != "" {
		cur = withSuffix(cur, b.ProfileID)
		plan = append(plan, cur)
	}
	if b.Flavor != "" && b.Flavor != "web" && b.ProfileFS != nil {
		if _, err := fs.Stat(b.ProfileFS, path.Join("image", "flavors", b.Flavor, "Dockerfile")); err == nil {
			plan = append(plan, withSuffix(cur, b.Flavor))
		}
	}
	return plan
}

// BuildDevImage собирает недостающие ступени и проверяет, что Target есть.
func BuildDevImage(ctx context.Context, b DevBuild) error {
	cli, err := localClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	// GuestServer не ставим (false): локальному режиму нужен guest только с
	// bridge — бинарь agctl-guest-linux-<arch> из BinDir.
	rt := &Runtime{cli: cli, cfg: Config{BinDir: b.BinDir, DshSkel: b.SkelFS,
		Version: b.Version, Commit: b.Commit, BuildDate: b.BuildDate}, imageFS: b.ImageFS}
	log := b.Log
	if log == nil {
		log = func(string) {}
	}
	// Образ реестра (ghcr.io/org/agctl-dsh:0.2.2), которого нет ни локально,
	// ни в реестре, собирается на месте под локальным именем и получает имя
	// реестра тегом: каталог профиля один и для машин с реестром, и без.
	if local := LocalImageName(b.Target); local != b.Target {
		inner := b
		inner.Target = local
		if err := BuildDevImage(ctx, inner); err != nil {
			return err
		}
		if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: local, Target: b.Target}); err != nil {
			return fmt.Errorf("docker: тег %s → %s: %w", local, b.Target, err)
		}
		if b.Log != nil {
			b.Log(fmt.Sprintf("✓ %s — собран на месте как %s (версию из реестра потом возьмёт agctl dev update)", b.Target, local))
		}
		return nil
	}
	plan := DevBuildPlan(b)
	if last := plan[len(plan)-1]; last != b.Target {
		return fmt.Errorf("образ %s так не собрать: из профиля %q и вкуса %q получается %s — проверьте image/flavor в config.yaml дома или возьмите образ из реестра", b.Target, b.ProfileID, b.Flavor, last)
	}
	exists := func(ref string) bool {
		_, err := cli.ImageInspect(ctx, ref)
		return err == nil
	}
	tag := imageTag(b.Target)
	base := "agctl-dsh:" + tag
	prev := ""
	for i, ref := range plan {
		if exists(ref) {
			log(fmt.Sprintf("✓ %s уже есть", ref))
			prev = ref
			continue
		}
		log(fmt.Sprintf("… [%d/%d] собираю %s", i+1, len(plan), ref))
		var err error
		switch {
		case ref == base:
			err = rt.buildBase(ctx, ref)
		case ref == "agctl-dsh-min:"+tag:
			err = rt.buildBaseVariant(ctx, ref, "min")
		case ref == "agctl-dsh-web:"+tag:
			err = rt.buildFromFS(ctx, b.ImageFS, "image/flavors/web", "Dockerfile", ref, prev)
		case b.Overlay != nil && ref == withSuffix(prev, b.ProfileID):
			err = rt.buildFromFS(ctx, b.Overlay, ".", b.OverlayDockerfile, ref, prev)
		default: // вкус профиля
			err = rt.buildFromFS(ctx, b.ProfileFS, path.Join("image", "flavors", b.Flavor), "Dockerfile", ref, prev)
		}
		if err != nil {
			return err
		}
		log(fmt.Sprintf("✓ %s собран", ref))
		prev = ref
	}
	return nil
}

// buildFromFS — docker build поддерева fsys (prefix) с BASE=base.
func (rt *Runtime) buildFromFS(ctx context.Context, fsys fs.FS, prefix, dockerfile, tag, base string) error {
	ctxDir, err := os.MkdirTemp("", "agctl-devimg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(ctxDir)
	if err := extractFS(fsys, prefix, ctxDir); err != nil {
		return fmt.Errorf("docker: контекст %s: %w", tag, err)
	}
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	return rt.buildContext(ctx, ctxDir, tag, dockerfile, map[string]*string{"BASE": &base})
}
