package dockerrt

// registry.go — откуда тянуть образы семейства agctl-dsh* с локальными
// именами (agctl-dsh:0.2.2, agctl-dsh-min:0.2.2): из реестра образов agctl
// (images.registry профиля, по умолчанию — реестр ядра, куда публикует
// .github/workflows/images.yml) под именем <реестр>/<имя>:<тег>, а на машине
// образ получает прежнее локальное имя тегом. Ссылки с реестром
// (ghcr.io/org/agctl-dsh:0.2.2) тянутся как есть.

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/client"
)

// DefaultRegistry — реестр образов agctl по умолчанию: туда публикует
// .github/workflows/images.yml (agctl-dsh и agctl-dsh-min, linux/amd64 и
// linux/arm64). Вкусов (-web) и надстроек профилей там нет — они
// собираются на машине.
const DefaultRegistry = "ghcr.io/arijjia"

// RegistryNone — images.registry: none — в реестр не ходить.
const RegistryNone = "none"

// ResolveRegistry — реестр по значению images.registry профиля: пусто —
// DefaultRegistry, "none" — "" (в реестр не ходить).
func ResolveRegistry(v string) string {
	v = strings.TrimSuffix(strings.TrimSpace(v), "/")
	switch {
	case v == "":
		return DefaultRegistry
	case strings.EqualFold(v, RegistryNone):
		return ""
	}
	return v
}

// LocalFamily — образ семейства agctl-dsh* с локальным именем, без реестра
// (agctl-dsh-web:0.2.2): такие тянутся из реестра образов agctl.
func LocalFamily(ref string) bool {
	return !strings.Contains(ref, "/") && strings.HasPrefix(ref, "agctl-dsh")
}

// RegistryRef — откуда тянуть образ ref: "agctl-dsh-min:0.2.2" и
// "ghcr.io/arijjia" → "ghcr.io/arijjia/agctl-dsh-min:0.2.2" (без тега —
// latest). "" — так не тянуть: реестр выключен (registry ""), ссылка уже с
// реестром (docker pull как есть) или образ не из семейства agctl-dsh.
func RegistryRef(ref, registry string) string {
	registry = strings.TrimSuffix(strings.TrimSpace(registry), "/")
	if registry == "" || !LocalFamily(ref) {
		return ""
	}
	if strings.Contains(ref, "@") {
		return registry + "/" + ref // по digest — как есть
	}
	name, tag := ref, "latest"
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		name, tag = ref[:i], ref[i+1:]
	}
	return registry + "/" + name + ":" + tag
}

// PullTagged — docker pull remote и тег local на скачанный образ
// (ghcr.io/arijjia/agctl-dsh:0.2.2 → agctl-dsh:0.2.2). Тег реестра остаётся:
// по нему видно, откуда образ. Образ работающего контейнера не меняется —
// новый тег подхватит следующий старт.
func PullTagged(ctx context.Context, remote, local string) error {
	if err := PullImage(ctx, remote); err != nil {
		return err
	}
	cli, err := localClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	if _, err := cli.ImageTag(ctx, client.ImageTagOptions{Source: remote, Target: local}); err != nil {
		return fmt.Errorf("docker: тег %s → %s: %w", remote, local, err)
	}
	return nil
}
