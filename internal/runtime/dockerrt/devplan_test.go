package dockerrt

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	names := func(items []ContextItem) string {
		var out []string
		for _, it := range items {
			out = append(out, it.Name)
		}
		return strings.Join(out, " ")
	}
	if got := names(base.Context); got != "dsh-net.mjs entrypoint.sh office2pdf skel/ agctl" {
		t.Fatalf("контекст: %s", got)
	}
	web, over := stages[1], stages[2]
	if web.Kind != "web" || web.Args["BASE"] != "agctl-dsh:0.2.3-alpha.2" || !strings.Contains(join(web.Downloads), "Chromium") {
		t.Fatalf("web: %+v", web)
	}
	if over.Kind != "profile" || over.Args["BASE"] != "agctl-dsh-web:0.2.3-alpha.2" || join(over.Apt) != "cowsay" || names(over.Context) != "certs/" {
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

// Контекст сборки — структурно: что за файл и откуда (встроен в бинарь
// agctl, скелет dsh-home, guest-бинарь на диске, файл профиля), размер, у
// каталога — число файлов; guest-бинарь — путь, размер и дата или «нет».
func TestDevBuildStagesContext(t *testing.T) {
	bin := t.TempDir()
	guest := []byte("\x7fELF\x02\x01\x01\x00 guest")
	if err := os.WriteFile(filepath.Join(bin, "agctl-guest-linux-arm64"), guest, 0o755); err != nil {
		t.Fatal(err)
	}
	b := DevBuild{Target: "agctl-dsh-org:0.2.2", ImageFS: assets.Image, SkelFS: assets.DshSkel, BinDir: bin, Arch: "arm64",
		ProfileID: "org", Overlay: fstest.MapFS{
			"Dockerfile":   {Data: []byte("ARG BASE\nFROM ${BASE}\nCOPY certs/ /usr/local/share/ca-certificates/\n")},
			"certs/ca.crt": {Data: []byte("-----BEGIN CERTIFICATE-----\n")},
			"certs/b.crt":  {Data: []byte("x")},
		}, OverlayDockerfile: "Dockerfile", OverlayDir: "image"}
	stages, err := DevBuildStages(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 {
		t.Fatalf("ступени: %+v", stages)
	}
	byName := map[string]ContextItem{}
	for _, it := range stages[0].Context {
		byName[it.Name] = it
	}
	ep := byName["entrypoint.sh"]
	if ep.Origin != CtxEmbedded || ep.From != "встроен в бинарь agctl" || ep.Path != "internal/assets/image/entrypoint.sh" || ep.Size == 0 || ep.Dir {
		t.Fatalf("entrypoint.sh: %+v", ep)
	}
	skel := byName["skel/"]
	if skel.Origin != CtxSkel || !skel.Dir || skel.Files < 5 || skel.Size == 0 || !strings.Contains(skel.From, "скелет dsh-home") || skel.Path != "internal/assets/dsh-skel/" {
		t.Fatalf("skel/: %+v", skel)
	}
	g := byName["agctl"]
	if g.Origin != CtxGuest || g.Missing || g.Path != filepath.Join(bin, "agctl-guest-linux-arm64") || g.Size != int64(len(guest)) || g.Modified == "" {
		t.Fatalf("guest: %+v", g)
	}
	over := stages[1].Context
	if len(over) != 1 || over[0].Name != "certs/" || !over[0].Dir || over[0].Files != 2 || over[0].Origin != CtxProfile ||
		over[0].From != "файл профиля org" || over[0].Path != "image/certs/" {
		t.Fatalf("надстройка: %+v", over)
	}
	// guest-бинаря под архитектуру демона нет — «нет» и что делать
	b.Arch = "amd64"
	stages, err = DevBuildStages(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range stages[0].Context {
		if it.Name == "agctl" && (!it.Missing || !strings.Contains(it.Note, "agctl-guest-linux-amd64") || it.Path != filepath.Join(bin, "agctl-guest-linux-amd64")) {
			t.Fatalf("нет guest: %+v", it)
		}
	}
}

// Файлы контекста для просмотра: текст встроенного файла, каталог скелета
// списком, файл профиля; guest-бинарь и большой файл — ошибка с размером;
// нет пути или ступени — fs.ErrNotExist; «..» — отказ.
func TestDevContextFile(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agctl-guest-linux-arm64"), bytes.Repeat([]byte("\x7fELF\x00\x01"), 1<<18), 0o755); err != nil {
		t.Fatal(err)
	}
	b := DevBuild{Target: "agctl-dsh-web-org:0.2.2", Flavor: "web", ImageFS: assets.Image, SkelFS: assets.DshSkel, BinDir: bin, Arch: "arm64",
		ProfileID: "org", Overlay: fstest.MapFS{
			"Dockerfile":   {Data: []byte("ARG BASE\nFROM ${BASE}\n")},
			"certs/ca.crt": {Data: []byte("сертификат\n")},
			"big.txt":      {Data: bytes.Repeat([]byte("a"), MaxContextText+1)},
		}, OverlayDockerfile: "Dockerfile"}
	f, err := DevContextFile(b, 1, "entrypoint.sh")
	if err != nil || !strings.HasPrefix(f.Text, "#!/bin/sh") || f.Size != int64(len(f.Text)) || f.Dir {
		t.Fatalf("entrypoint.sh: %+v %v", f.Path, err)
	}
	f, err = DevContextFile(b, 1, "skel/")
	if err != nil || !f.Dir || len(f.Files) < 5 || f.Size == 0 {
		t.Fatalf("skel: %+v %v", f, err)
	}
	found := false
	for _, e := range f.Files {
		found = found || e.Path == "skel/profiles/web/package.json"
	}
	if !found {
		t.Fatalf("в списке skel нет skel/profiles/web/package.json: %+v", f.Files)
	}
	if f, err := DevContextFile(b, 1, "skel/profiles/web/package.json"); err != nil || !strings.Contains(f.Text, "{") {
		t.Fatalf("файл скелета: %+v %v", f, err)
	}
	if _, err := DevContextFile(b, 1, "agctl"); err == nil || !strings.Contains(err.Error(), "двоичный") || !strings.Contains(err.Error(), "1.5 МБ") {
		t.Fatalf("guest-бинарь: %v", err)
	}
	if f, err := DevContextFile(b, 2, "Dockerfile"); err != nil || !strings.Contains(f.Text, "playwright") {
		t.Fatalf("Dockerfile вкуса web: %v", err)
	}
	if f, err := DevContextFile(b, 3, "certs/ca.crt"); err != nil || f.Text != "сертификат\n" {
		t.Fatalf("файл профиля: %+v %v", f, err)
	}
	if _, err := DevContextFile(b, 3, "big.txt"); err == nil || !strings.Contains(err.Error(), "256.0 КБ") {
		t.Fatalf("большой файл: %v", err)
	}
	for _, c := range []struct {
		stage int
		rel   string
	}{{1, "nope.sh"}, {3, "skel/x"}, {4, "Dockerfile"}, {0, "entrypoint.sh"}} {
		if _, err := DevContextFile(b, c.stage, c.rel); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("ступень %d, %s: ждали fs.ErrNotExist, получили %v", c.stage, c.rel, err)
		}
	}
	for _, rel := range []string{"../etc/passwd", "/etc/passwd", "", "."} {
		if _, err := DevContextFile(b, 1, rel); err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%q: ждали отказ, получили %v", rel, err)
		}
	}
}
