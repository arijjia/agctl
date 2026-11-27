package dockerrt

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/arijjia/agctl/internal/assets"
)

// План сборки: ступени в порядке BuildDevImage, у каждой — Dockerfile,
// аргументы и выжимка (базовые образы, apt, npm, загрузки); тег-версия dsh
// уходит в DSH_VERSION.
func TestDevBuildStages(t *testing.T) {
	b := DevBuild{Target: "agctl-dsh-web-org:0.2.3-alpha.2", Flavor: "web", ImageFS: assets.Image, SkelFS: assets.DshSkel,
		ProfileID: "org", Overlay: fstest.MapFS{"Dockerfile": {Data: []byte("ARG BASE\nFROM ${BASE}\nRUN apt-get update && apt-get install -y --no-install-recommends cowsay\n")}, "certs/ca.crt": {Data: []byte("x")}},
		OverlayDockerfile: "Dockerfile", Version: "1.2.3", Commit: "abc1234"}
	stages, err := DevBuildStages(b)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, s := range stages {
		refs = append(refs, s.Ref)
	}
	if got := strings.Join(refs, " → "); got != "agctl-dsh:0.2.3-alpha.2 → agctl-dsh-web:0.2.3-alpha.2 → agctl-dsh-web-org:0.2.3-alpha.2" {
		t.Fatalf("ступени: %s", got)
	}
	base := stages[0]
	if base.Kind != "base" || base.Args["DSH_VERSION"] != "0.2.3-alpha.2" || base.Args["AGCTL_COMMIT"] != "abc1234" {
		t.Fatalf("базовый: %+v", base.Args)
	}
	join := func(xs []string) string { return strings.Join(xs, " ") }
	if !strings.Contains(join(base.From), "node:22-bookworm-slim") || strings.Contains(join(base.From), "base") {
		t.Fatalf("FROM базового — внешний образ, без внутренних стадий: %v", base.From)
	}
	if !strings.Contains(join(base.Apt), "libreoffice-impress") || !strings.Contains(join(base.Npm), "@deepseek-ai/dsh@0.2.3-alpha.2") {
		t.Fatalf("apt/npm: %v / %v", base.Apt, base.Npm)
	}
	if !strings.Contains(join(base.Npm), "@upstash/context7-mcp (прогрев npx)") {
		t.Fatalf("прогрев npx: %v", base.Npm)
	}
	if !strings.Contains(join(base.Downloads), "dl.k8s.io/release/v") || strings.Contains(join(base.Downloads), "${KUBECTL_VERSION}") {
		t.Fatalf("загрузки с подставленными версиями: %v", base.Downloads)
	}
	if strings.Contains(join(base.Downloads), "github.com/arijjia/agctl ") || strings.HasSuffix(join(base.Downloads), "github.com/arijjia/agctl") {
		t.Fatalf("адрес из LABEL — не загрузка: %v", base.Downloads)
	}
	if !strings.Contains(join(base.Context), "entrypoint.sh") || !strings.Contains(join(base.Context), "skel/") {
		t.Fatalf("контекст: %v", base.Context)
	}
	web, over := stages[1], stages[2]
	if web.Kind != "web" || web.Args["BASE"] != "agctl-dsh:0.2.3-alpha.2" || !strings.Contains(join(web.Downloads), "Chromium") {
		t.Fatalf("web: %+v", web)
	}
	if over.Kind != "profile" || over.Args["BASE"] != "agctl-dsh-web:0.2.3-alpha.2" || join(over.Apt) != "cowsay" || join(over.Context) != "certs/" {
		t.Fatalf("надстройка: %+v", over)
	}
	// минимальный без тега-версии — пин сборки
	min, err := DevBuildStages(DevBuild{Target: "agctl-dsh-min:0.2.2", Flavor: "min", ImageFS: assets.Image})
	if err != nil || len(min) != 1 || min[0].Kind != "min" || !strings.Contains(min[0].Args["DSH_VERSION"], assets.PinnedDshVersion()) {
		t.Fatalf("минимальный: %+v %v", min, err)
	}
}

// Переменные набора сертификатов: Node — всегда, остальные — для полного.
func TestCAEnv(t *testing.T) {
	if got := strings.Join(CAEnv(false), " "); got != "NODE_EXTRA_CA_CERTS="+ContainerCAFile {
		t.Fatalf("неполный: %s", got)
	}
	full := strings.Join(CAEnv(true), " ")
	for _, k := range []string{"SSL_CERT_FILE=", "REQUESTS_CA_BUNDLE=", "GIT_SSL_CAINFO=", "NODE_EXTRA_CA_CERTS="} {
		if !strings.Contains(full, k+ContainerCAFile) {
			t.Fatalf("полный без %s: %s", k, full)
		}
	}
}
