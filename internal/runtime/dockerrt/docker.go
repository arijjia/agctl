// Package dockerrt — реализация Runtime над Docker Engine API (нативный
// клиент moby/moby/client: unix socket на mac/linux, named pipe на Windows).
// Образ рабочего места строится самим agctl из встроенного контекста
// (Dockerfile, relay, скелет DSH_HOME) + linux-сборки agctl-guest своего
// варианта (карточка guest-image-flavours): серверные образы agctl-dsh-server*
// несут guest `-tags server` (dist/agctl-guest-server-linux-<arch>), локальные
// agctl-dsh* — guest без тегов, только bridge (dist/agctl-guest-linux-<arch>;
// в образе бинарь — /usr/local/bin/agctl).
package dockerrt

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"github.com/arijjia/agctl/internal/assets"
	"io"
	"io/fs"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// Контекст образа приходит из internal/assets (fs.FS с префиксом "image").

// Config — параметры рантайма.
type Config struct {
	Image       string // agctl-dsh:<tag>
	MemoryBytes int64  // лимит памяти контейнера
	NanoCPUs    int64  // лимит CPU
	BinDir      string // каталог с linux-сборками agctl (dist/)
	Home        string // local Docker adapter storage root; K8s adapters use PVCs instead
	DshSkel     fs.FS  // embedded DSH home seed for lazy Chat runtimes
	Version     string // версия agctl (метка контейнера для диагностики дрейфа)
	Commit      string // сборка agctl (метка образа io.agctl.guest.revision при сборке на месте)
	BuildDate   string // дата сборки agctl (метка образа org.opencontainers.image.created)

	// Надстройка организации (профиль images.dockerfile): Image собирается
	// из Overlay (Dockerfile = OverlayDockerfile) с build-arg BASE=BaseImage,
	// а BaseImage — из встроенного контекста. Пусто = Image и есть базовый.
	BaseImage         string
	Overlay           fs.FS
	OverlayDockerfile string

	// GuestServer — серверный вариант guest-бинаря в образе (build-arg
	// GUEST_FLAVOR=server, agctl-guest-server-linux-<arch>): ax-runner и
	// init-chat-home. agctl-server ставит true; сборка локального режима
	// (BuildDevImage) оставляет false — там guest только с bridge.
	GuestServer bool

	// Упрочнение контейнера (карточка dockerrt-hardening-runtime).
	// ContainerRuntime — OCI-рантайм Docker (HostConfig.Runtime): пусто = runc
	// демона, "runsc" (gVisor), "kata" — изоляция ядра; наличие проверяется
	// в New по docker info. Ставит только agctl-server.
	ContainerRuntime string
	// PidsLimit — потолок процессов контейнера; 0 = defaultPidsLimit.
	PidsLimit int64
	// NoHardening — выключить CapDrop/no-new-privileges/PidsLimit (аварийный
	// откат, если надстройке профиля нужны снятые capabilities).
	NoHardening bool

	// EgressAllow — куда ещё, кроме провайдеров модели, контейнеру можно
	// ходить при egress-allowlist: хосты, IPv4 или CIDR (профиль
	// egress.allow + флаг --egress-allow). Хосты резолвятся на хосте при
	// старте контейнера и пинятся в /etc/hosts, как провайдеры модели.
	EgressAllow []string
}

// Метки контейнеров agctl.
const (
	labelWorkspace    = "agctl.workspace"     // владение: контейнер создан agctl
	labelVersion      = "agctl.version"       // версия agctl, создавшего контейнер
	labelImageDesired = "agctl.image.desired" // желаемый образ (лаунчер developer'а)
)

// Runtime — docker-реализация.
type Runtime struct {
	cli     *client.Client
	cfg     Config
	imageFS fs.FS // контекст образа (префикс "image")

	// egress — адреса, уже разрешённые в цепочке дома (объединение по всем
	// контейнерам: у пользователей бывают разные провайдеры).
	egressMu      sync.Mutex
	egressAllowed map[string]bool
}

// New подключается к Docker (env/дефолтные сокеты). imageFS — контекст
// сборки образа (fs.FS с префиксом "image", из internal/assets).
func New(cfg Config, imageFS fs.FS) (*Runtime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: client: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ping, err := cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: демон недоступен (запустите Docker Desktop / dockerd): %w", err)
	}
	if cfg.BinDir == "" {
		cfg.BinDir = "dist"
	}
	if cfg.Image == "" {
		// Единственный вызов New — agctl-server (сборка локального режима
		// идёт через BuildDevImage): серверный образ рабочего места.
		cfg.Image = "agctl-dsh-server:0.2.2"
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	_ = ping
	if cfg.ContainerRuntime != "" {
		info, err := cli.Info(ctx, client.InfoOptions{})
		if err != nil {
			return nil, fmt.Errorf("docker: info (проверка рантайма %s): %w", cfg.ContainerRuntime, err)
		}
		if err := checkContainerRuntime(cfg.ContainerRuntime, info.Info.Runtimes); err != nil {
			return nil, err
		}
	}
	return &Runtime{cli: cli, cfg: cfg, imageFS: imageFS}, nil
}

// Close освобождает клиент.
func (rt *Runtime) Close() error { return rt.cli.Close() }

// ── образ ────────────────────────────────────────────────────────────────────

// EnsureImage строит образ рабочего места, если его нет. Требует linux-сборку
// agctl-guest своего варианта в cfg.BinDir (make go_linux / go_linux_server).
func (rt *Runtime) EnsureImage(ctx context.Context) error {
	if _, err := rt.cli.ImageInspect(ctx, rt.cfg.Image); err == nil {
		return nil
	}
	if rt.cfg.Overlay == nil || rt.cfg.BaseImage == "" || rt.cfg.BaseImage == rt.cfg.Image {
		return rt.buildBase(ctx, rt.cfg.Image)
	}
	if _, err := rt.cli.ImageInspect(ctx, rt.cfg.BaseImage); err != nil {
		if err := rt.buildBase(ctx, rt.cfg.BaseImage); err != nil {
			return err
		}
	}
	return rt.buildOverlay(ctx)
}

// buildOverlay собирает образ организации из контекста профиля поверх
// базового (ARG BASE в Dockerfile надстройки).
func (rt *Runtime) buildOverlay(ctx context.Context) error {
	ctxDir, err := os.MkdirTemp("", "agctl-overlay-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(ctxDir)
	if err := extractFS(rt.cfg.Overlay, ".", ctxDir); err != nil {
		return fmt.Errorf("docker: контекст надстройки профиля: %w", err)
	}
	dockerfile := rt.cfg.OverlayDockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	base := rt.cfg.BaseImage
	return rt.buildContext(ctx, ctxDir, rt.cfg.Image, dockerfile, map[string]*string{"BASE": &base})
}

// buildBase собирает базовый образ рабочего места из встроенного контекста
// под тегом tag (вариант guest — по cfg.GuestServer).
func (rt *Runtime) buildBase(ctx context.Context, tag string) error {
	return rt.buildBaseVariant(ctx, tag, "")
}

// buildBaseVariant — базовый образ из встроенного контекста: variant ""
// — полный (Dockerfile), "min" — минимальный (Dockerfile.min). Тег-версия
// dsh (agctl-dsh:0.2.3-alpha.2) собирает образ на этой версии dsh;
// минимальному без такого тега передаётся пин из Dockerfile. Метки образа
// (сборка agctl, дата) — как у make img_build.
func (rt *Runtime) buildBaseVariant(ctx context.Context, tag, variant string) error {
	var bin string
	var err error
	if rt.cfg.GuestServer {
		bin, err = findServerLinuxBinary(rt.cfg.BinDir, rt.daemonArch(ctx))
	} else {
		bin, err = findLinuxBinary(rt.cfg.BinDir, rt.daemonArch(ctx))
	}
	if err != nil {
		return err
	}
	ctxDir, err := os.MkdirTemp("", "agctl-image-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(ctxDir)
	if err := extractFS(rt.imageFS, "image", ctxDir); err != nil {
		return fmt.Errorf("docker: контекст образа: %w", err)
	}
	// COPY skel/ в Dockerfile: скелет DSH_HOME (как .build-img/skel в make).
	skel := filepath.Join(ctxDir, "skel")
	if err := os.MkdirAll(skel, 0o755); err != nil {
		return err
	}
	if rt.cfg.DshSkel != nil {
		if err := extractFS(rt.cfg.DshSkel, "dsh-skel", skel); err != nil {
			return fmt.Errorf("docker: скелет DSH_HOME: %w", err)
		}
	}
	dst := filepath.Join(ctxDir, "agctl")
	if err := copyFile(bin, dst); err != nil {
		return fmt.Errorf("docker: копирование %s: %w", bin, err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	args := rt.labelArgs()
	if rt.cfg.GuestServer {
		server := "server"
		args["GUEST_FLAVOR"] = &server
	}
	dsh := assets.DshVersionFromTag(imageTag(tag))
	dockerfile := "Dockerfile"
	if variant == "min" {
		dockerfile = "Dockerfile.min"
		if dsh == "" {
			dsh = assets.PinnedDshVersion()
		}
	}
	if dsh != "" {
		args["DSH_VERSION"] = &dsh
	}
	return rt.buildContext(ctx, ctxDir, tag, dockerfile, args)
}

// labelArgs — build-arg'и меток образа: версия и сборка agctl, дата.
func (rt *Runtime) labelArgs() map[string]*string {
	args := map[string]*string{}
	set := func(k, v string) {
		if v != "" {
			v := v
			args[k] = &v
		}
	}
	set("AGCTL_VERSION", rt.cfg.Version)
	set("AGCTL_COMMIT", rt.cfg.Commit)
	set("AGCTL_BUILD_DATE", rt.cfg.BuildDate)
	return args
}

// buildContext — docker build каталога ctxDir под тегом tag.
func (rt *Runtime) buildContext(ctx context.Context, ctxDir, tag, dockerfile string, args map[string]*string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tarDir(ctxDir, tw); err != nil {
		return fmt.Errorf("docker: tar: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	res, err := rt.cli.ImageBuild(ctx, &buf, client.ImageBuildOptions{
		Tags:        []string{tag},
		Dockerfile:  dockerfile,
		BuildArgs:   args,
		Remove:      true,
		ForceRemove: true,
	})
	if err != nil {
		return fmt.Errorf("docker: build: %w", err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.Body.Close() != nil {
		_ = out
	}
	if _, err := rt.cli.ImageInspect(ctx, tag); err != nil {
		return fmt.Errorf("docker: образ %s не собрался; вывод:\n%s", tag, tail(string(out), 4000))
	}
	return nil
}

// findLinuxBinary ищет linux-сборку agctl-guest ЛОКАЛЬНОГО варианта (только
// bridge) — бинарь образов agctl-dsh* локального режима, — под архитектуру
// демона Docker (arch: arm64|amd64; "" — любая). Где ищем, по порядку:
//   - binDir (локальный режим: <корень>/bin/guest — туда кладут make install
//     и установщики; сервер из исходников: dist/, make go_linux / img_build);
//   - рядом с запущенным бинарём (распакованный релиз: agctl и
//     agctl-guest-linux-* в одной папке — образ собирается без исходников);
//   - <папка бинаря>/guest (раскладка корня: bin/agctl и bin/guest/…).
//
// Имя одно: agctl-guest-linux-<arch>.
func findLinuxBinary(binDir, arch string) (string, error) {
	return findLinuxBinaryNamed(binDir, arch,
		[]string{"agctl-guest-linux-"},
		"agctl-guest-linux-<arm64|amd64>", "make go_linux")
}

// findServerLinuxBinary ищет linux-сборку agctl-guest СЕРВЕРНОГО варианта
// (`-tags server`: ax-runner, init-chat-home) — бинарь образов
// agctl-dsh-server* (рантаймы docker|k8s|ax agctl-server). Локальный
// agctl-guest-linux-* сюда попадать не должен: без ax-runner/init-chat-home
// серверные рантаймы молча ломаются, поэтому fallback на него нет.
func findServerLinuxBinary(binDir, arch string) (string, error) {
	return findLinuxBinaryNamed(binDir, arch,
		[]string{"agctl-guest-server-linux-"},
		"agctl-guest-server-linux-<arm64|amd64>", "make go_linux_server")
}

// findLinuxBinaryNamed — общий поиск по префиксам имён в стандартных каталогах
// (binDir, рядом с бинарём, <папка бинаря>/guest); hint — что собрать, если не нашли.
func findLinuxBinaryNamed(binDir, arch string, prefixes []string, want, hint string) (string, error) {
	archs := []string{"arm64", "amd64"}
	if arch != "" {
		archs = []string{arch}
	}
	dirs := []string{binDir}
	if self, _ := os.Executable(); self != "" {
		if r, err := filepath.EvalSymlinks(self); err == nil {
			self = r
		}
		dirs = append(dirs, filepath.Dir(self), filepath.Join(filepath.Dir(self), "guest"))
	}
	for _, a := range archs {
		for _, prefix := range prefixes {
			for _, dir := range dirs {
				if dir == "" {
					continue
				}
				p := filepath.Join(dir, prefix+a)
				if st, err := os.Stat(p); err == nil && !st.IsDir() {
					return p, nil
				}
			}
		}
	}
	if arch != "" {
		want = want[:len(want)-len("<arm64|amd64>")] + arch
	}
	return "", fmt.Errorf("нет linux-сборки %s ни в %s, ни рядом с бинарём: положите её в <корень>/bin/guest (~/.agctl/bin/guest; есть в релизе, ставит установщик) или выполните `%s`", want, binDir, hint)
}

// daemonArch — архитектура демона Docker в терминах Go (arm64|amd64; "" —
// не узнали).
func (rt *Runtime) daemonArch(ctx context.Context) string {
	res, err := rt.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return ""
	}
	return goArch(res.Info.Architecture)
}

// goArch — архитектура в терминах Go и образов (arm64|amd64; "" — другая):
// x86_64 → amd64, aarch64 → arm64.
func goArch(a string) string {
	switch strings.ToLower(a) {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	}
	return ""
}

// extractFS распаковывает поддерево embed-FS (префикс prefix) в каталог dst:
// каталоги 0755, файлы 0644.
func extractFS(fsys fs.FS, prefix, dst string) error {
	return fs.WalkDir(fsys, prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(prefix, path)
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// copyFile копирует файл src → dst (права — как у исходника).
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// tarDir — детерминированный tar каталога (для build context).
func tarDir(dir string, tw *tar.Writer) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && filepath.Clean(path) != filepath.Clean(dir) {
				rel, _ := filepath.Rel(dir, path)
				hdr := &tar.Header{Name: filepath.ToSlash(rel), Typeflag: tar.TypeDir, Mode: 0o755, ModTime: time.Unix(0, 0)}
				return tw.WriteHeader(hdr)
			}
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.Mode = 0o755
		hdr.ModTime = time.Unix(0, 0)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

// tail возвращает последние n байт строки (ограниченные превью логов).
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// ── жизненный цикл воркспейса ────────────────────────────────────────────────

// StartResult — что вернул запуск контейнера.
type StartResult struct {
	ContainerID string
	HostPort    int
	// SSHPort — хост-порт SSH (роль developer); 0 = sshd не публиковался.
	SSHPort int
}

// StartOpts — параметры публикации контейнера: порт dsh web и, опционально,
// SSH-канал (ключ передаётся env в контейнер, порт 2222 публикуется на loopback).
type StartOpts struct {
	// ExtraBinds — доп. bind-маунты "host:/workspace/<имя>" (несколько папок
	// рабочего места локального режима) и "host:host" (пути как на хосте).
	ExtraBinds []string
	// WorkingDir — рабочий каталог процесса контейнера ("" — WORKDIR образа).
	WorkingDir string
	// Env — переменные, перекрывающие базовые (HOME и т.п.) и дополняющие.
	Env         []string
	HostPort    int
	SSHPubKey   string   // пусто = sshd не поднимаем
	SSHHostPort int      // 0 = не публиковать (осмысленно только с SSHPubKey)
	Image       string   // желаемый образ (лаунчер developer'а); пусто = rt.cfg.Image
	DevEnv      []string // доменные переменные контейнера developer'а (KEY=VALUE); пусто = не добавлять
	// EgressAllowlist — посадить контейнер на выделенную сеть и запретить
	// egress наружу, кроме DNS (карточка egress-allowlist; opt-in флаг serve).
	EgressAllowlist bool
	// User — "uid:gid" процесса контейнера ("" — пользователь образа);
	// применяется, только если образ это умеет (imageRunsAsUser).
	User string
}

// Start создаёт и запускает контейнер воркспейса с заданными портами.
// Монтирует рабочий клон и DSH_HOME. Ключи LLM в env контейнера не кладутся:
// dsh берёт их из DSH_HOME/.credentials.yaml (0600, пишет
// provision.RefreshProvider; у dsh-credentials-local файл главнее env).
func (rt *Runtime) Start(ctx context.Context, ws provision.Layout, wsID string, opts StartOpts, p provision.Provider) (StartResult, error) {
	if err := rt.EnsureImage(ctx); err != nil {
		return StartResult{}, err
	}
	// Лаунчер developer'а: желаемый образ из opts перекрывает дефолт.
	// Отсутствующий на хосте образ тянем из реестра (например, Nexus).
	desired := opts.Image
	if desired == "" {
		desired = rt.cfg.Image
	}
	if desired != rt.cfg.Image {
		if err := rt.ensureImageRef(ctx, desired); err != nil {
			return StartResult{}, fmt.Errorf("docker: желаемый образ %s недоступен: %w", desired, err)
		}
	}
	// Дрейф версий: контейнер прошлой сборки (пережил рестарт серва) раздавал
	// бы старый dsh web, хотя make up пересобрал образ под тем же тегом.
	// Сравниваем фактический образ контейнера с ЖЕЛАЕМЫМ (не глобальным) и
	// бонсим дрейф: developer сменил образ через лаунчер — пересоздаём.
	imageID := rt.imageIDByRef(ctx, desired)
	name := "agctl-" + wsID
	if imageID != "" {
		stale, err := rt.removeDriftedByName(ctx, name, imageID)
		if err != nil {
			log.Printf("[dockerrt] %s: проверка дрейфа версии: %v", wsID, err)
		} else if stale {
			log.Printf("[dockerrt] %s: контейнер собран не желаемым образом — пересоздаю (дрейф версии; данные на маунтах не тронуты)", wsID)
		}
	}
	// гонки за порт: docker сам отдаст ошибку, если порт занят.
	// Все Base URL могут иметь разные split-DNS corporate hosts; резолвим
	// каждый на хосте и добавляем --add-host при создании контейнера.
	extraHosts := []string{"host.docker.internal:host-gateway"}
	pins, pinErrors := pinsForBaseURLs(p.AllProviderURLs(), net.LookupIP)
	for _, err := range pinErrors {
		log.Printf("[dockerrt] %s: %v — для этого LLM endpoint нет DNS-пина (проверьте VPN)", wsID, err)
	}
	extraHosts = append(extraHosts, pins...)
	env := overrideEnv(containerEnv(wsID, opts.DevEnv), opts.Env)
	portBindings := network.PortMap{
		network.MustParsePort("3080/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: fmt.Sprint(opts.HostPort)}},
	}
	// Вариант образа «без SSH» (ENV AGCTL_NO_SSHD=1 в образе, лаунчер
	// developer'а): ключ не передаём и порт 2222 не публикуем — иначе снаружи
	// висит порт, внутри которого никто не слушает.
	noSSHD := desired != rt.cfg.Image && rt.imageNoSSHD(ctx, desired)
	if opts.SSHPubKey != "" && opts.SSHHostPort > 0 && !noSSHD {
		env = append(env, "AGCTL_SSH_PUBKEY="+opts.SSHPubKey)
		portBindings[network.MustParsePort("2222/tcp")] = []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: fmt.Sprint(opts.SSHHostPort)}}
	}
	if opts.User != "" && !rt.imageRunsAsUser(ctx, desired) {
		log.Printf("[dockerrt] %s: образ %s старее guest API %d — контейнер под root, как раньше (agctl dev update скачает новый)", wsID, desired, imageUserAPI)
		opts.User = ""
	}
	cfg := client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:      desired,
			User:       opts.User,
			Env:        env,
			WorkingDir: opts.WorkingDir,
			Labels: map[string]string{
				labelWorkspace:    wsID,
				labelVersion:      rt.cfg.Version,
				labelImageDesired: desired,
			},
		},
		HostConfig: &container.HostConfig{
			Binds: append([]string{
				ws.Work + ":/workspace",
				ws.DshHome + ":/home/dsh",
			}, opts.ExtraBinds...),
			// host-gateway: из контейнера должен резолвиться
			// host.docker.internal (корпоративный LLM-шлюз на хосте).
			ExtraHosts:   extraHosts,
			PortBindings: portBindings,
			Resources: container.Resources{
				Memory:   rt.cfg.MemoryBytes,
				NanoCPUs: rt.cfg.NanoCPUs,
			},
			RestartPolicy: container.RestartPolicy{Name: "no"},
		},
	}
	rt.applyHardening(cfg.HostConfig)
	// Egress-allowlist (opt-in): выделенная сеть + DOCKER-USER правила до
	// старта контейнера, чтобы ни один пакет не ушёл мимо запрета.
	if opts.EgressAllowlist {
		netName, subnet, gateway, err := rt.ensureEgressNetwork(ctx)
		if err != nil {
			return StartResult{}, err
		}
		allowPins, allowCIDRs, allowErrs := egressTargets(rt.cfg.EgressAllow, net.LookupIP)
		for _, err := range allowErrs {
			log.Printf("[dockerrt] %s: egress-allow: %v", wsID, err)
		}
		cfg.HostConfig.ExtraHosts = append(cfg.HostConfig.ExtraHosts, allowPins...)
		allowed := rt.egressUnion(append(allowCIDRs, pinIPs(append(pins, allowPins...))...))
		if err := rt.applyEgressRules(ctx, egressChainName(rt.cfg.Home), subnet, gateway, allowed); err != nil {
			return StartResult{}, err
		}
		cfg.HostConfig.NetworkMode = container.NetworkMode(netName)
	}
	res, err := rt.cli.ContainerCreate(ctx, cfg)
	if err != nil {
		// контейнер с таким именем мог остаться от прошлого прогона — уберём и повторим
		if strings.Contains(err.Error(), "already in use") {
			_ = rt.removeByName(ctx, name)
			res, err = rt.cli.ContainerCreate(ctx, cfg)
		}
		if err != nil {
			return StartResult{}, fmt.Errorf("docker: create: %w", err)
		}
	}
	if _, err := rt.cli.ContainerStart(ctx, res.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = rt.cli.ContainerRemove(ctx, res.ID, client.ContainerRemoveOptions{Force: true})
		return StartResult{}, fmt.Errorf("docker: start: %w", err)
	}
	sshPort := 0
	if opts.SSHPubKey != "" && opts.SSHHostPort > 0 && !noSSHD {
		sshPort = opts.SSHHostPort
	}
	return StartResult{ContainerID: res.ID, HostPort: opts.HostPort, SSHPort: sshPort}, nil
}

// Resume запускает существующий (остановленный) контейнер.
func (rt *Runtime) Resume(ctx context.Context, containerID string) error {
	_, err := rt.cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	return err
}

// ── дрейф версий ─────────────────────────────────────────────────────────────

// currentImageID — ID образа под текущим тегом; "" если инспект недоступен
// (дрейф-контроль в этом запуске деградирует до прежнего поведения).
// ID, а не тег/строку версии: make up пересобирает образ под тем же тегом
// (agctl-dsh:0.2.2), и только ID отличает свежую сборку от прошлой.
func (rt *Runtime) currentImageID(ctx context.Context) string {
	insp, err := rt.cli.ImageInspect(ctx, rt.cfg.Image)
	if err != nil {
		return ""
	}
	return insp.ID
}

// imageIDByRef — sha образа по ссылке (тег/дайджест); "" если не найден.
func (rt *Runtime) imageIDByRef(ctx context.Context, ref string) string {
	insp, err := rt.cli.ImageInspect(ctx, ref)
	if err != nil {
		return ""
	}
	return insp.ID
}

// imageNoSSHD — образ собран с ENV AGCTL_NO_SSHD=1 (вариант «без SSH»
// лаунчера developer'а): entrypoint не поднимает sshd даже с ключом.
func (rt *Runtime) imageNoSSHD(ctx context.Context, ref string) bool {
	insp, err := rt.cli.ImageInspect(ctx, ref)
	if err != nil {
		return false
	}
	for _, e := range insp.Config.Env {
		if e == "AGCTL_NO_SSHD=1" {
			return true
		}
	}
	return false
}

// ensureImageRef — желаемый образ должен существовать локально: если нет —
// тянем из реестра (Nexus организации и пр.); недоступный реестр — ошибка
// старта, а не молчаливый откат на дефолтный образ.
func (rt *Runtime) ensureImageRef(ctx context.Context, ref string) error {
	if _, err := rt.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	pr, err := rt.cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	defer pr.Close()
	if err := pr.Wait(ctx); err != nil {
		return fmt.Errorf("pull: %w", err)
	}
	if _, err := rt.cli.ImageInspect(ctx, ref); err != nil {
		return fmt.Errorf("образ не найден после pull: %w", err)
	}
	return nil
}

// removeDriftedByName удаляет контейнер name, если он работает на образе,
// отличном от текущего тега. true — контейнер удалён как дрейф.
// InspectResponse.Image здесь — sha образа (не символическое имя, как в Summary).
func (rt *Runtime) removeDriftedByName(ctx context.Context, name, currentImageID string) (bool, error) {
	insp, err := rt.cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if !agctlDrifted(insp.Container.Image, currentImageID) {
		return false, nil
	}
	_, err = rt.cli.ContainerRemove(ctx, insp.Container.ID, client.ContainerRemoveOptions{Force: true})
	return true, err
}

// SweepVersionDrift удаляет контейнеры agctl (метка agctl.workspace) с именами
// по префиксу — чат-рантаймы этого дома, — работающие на образе не текущего
// тега. Хвосты убитого серва переживают рестарт и продолжают раздавать старый
// dsh web; свип на старте serve сносит их заранее, /chat/ пересоздаёт среды
// лениво (данные — на маунтах, не в контейнере). Возвращает число удалённых;
// ошибки отдельных удалений пишутся в лог и не прерывают свип.
func (rt *Runtime) SweepVersionDrift(ctx context.Context, namePrefix string) int {
	defaultImageID := rt.currentImageID(ctx)
	if defaultImageID == "" {
		return 0
	}
	list, err := rt.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		log.Printf("[dockerrt] свип дрейфа версий: %v", err)
		return 0
	}
	// Лаунчер developer'а: у контейнера с меткой agctl.image.desired свой
	// эталон образа — глобальный свип его не сносит (пересоздание по желаемому
	// образу делает Start при следующем запуске).
	desiredIDs := map[string]string{}
	removed := 0
	for _, c := range list.Items {
		// В Summary.Image — символическое имя (как в docker ps), sha живёт в
		// ImageID; в InspectResponse — наоборот, Image это sha. Не перепутать.
		if !sweepCandidate(c.Names, c.ImageID, c.Labels, namePrefix, defaultImageID, func(ref string) string {
			if id, ok := desiredIDs[ref]; ok {
				return id
			}
			id := rt.imageIDByRef(ctx, ref)
			desiredIDs[ref] = id
			return id
		}) {
			continue
		}
		if _, err := rt.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
			log.Printf("[dockerrt] свип дрейфа: удаление %s: %v", c.ID, err)
			continue
		}
		removed++
		log.Printf("[dockerrt] свип дрейфа: контейнер %v собран не желаемым образом — удалён (среда пересоздастся лениво)", c.Names)
	}
	return removed
}

// agctlDrifted — контейнер работает на образе, отличном от текущего тега
// (sha-префиксы нормализуем: одна и та же суть в разных полях API). Неизвестный
// текущий образ — решения нет: дрейфом не считаем.
func agctlDrifted(containerImage, currentImageID string) bool {
	if currentImageID == "" {
		return false
	}
	return imageDigest(containerImage) != imageDigest(currentImageID)
}

// sweepCandidate — кандидата свипа удалять: контейнер наш (метка
// agctl.workspace), чей-то чужой контейнер с подходящим именем не трогаем;
// работает не на ЖЕЛАЕМОМ образе (containerImageID — sha из Summary.ImageID);
// имя попадает под префикс дома. Эталон контейнера: метка
// agctl.image.desired (лаунчер), иначе — глобальный текущий образ
// (defaultImageID; образы контейнеров без метки пересоздаём как раньше).
// resolveID вытаскивает sha по ссылке желаемого образа; пусто = образ не
// найден — такой контейнер не считаем дрейфом (критерия нет, трогать опасно).
func sweepCandidate(names []string, containerImageID string, labels map[string]string, namePrefix, defaultImageID string, resolveID func(string) string) bool {
	if defaultImageID == "" || labels[labelWorkspace] == "" {
		return false
	}
	reference := defaultImageID
	if ref := labels[labelImageDesired]; ref != "" {
		resolve := resolveID(ref)
		if resolve == "" {
			return false
		}
		reference = resolve
	}
	if !agctlDrifted(containerImageID, reference) {
		return false
	}
	for _, n := range names {
		if strings.HasPrefix(strings.TrimPrefix(n, "/"), namePrefix) {
			return true
		}
	}
	return false
}

// imageDigest нормализует ссылку на образ к голому hex-дайджесту.
func imageDigest(ref string) string {
	return strings.TrimPrefix(strings.TrimSpace(ref), "sha256:")
}

// Stop останавливает и удаляет контейнер воркспейса: контейнер эфемерен,
// состояние (репка, дом dsh) живёт на маунтах хоста. Удаление при остановке
// нужно, чтобы следующий Start пересоздал контейнер с актуальной конфигурацией —
// пином корпоративного хоста, обновлённым образом или env.
func (rt *Runtime) Stop(ctx context.Context, containerID string) error {
	timeout := 10
	if _, err := rt.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout}); err != nil && !isNotFound(err) {
		return err
	}
	if _, err := rt.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

// pinnedHost — запись "host:ip" для ExtraHosts по baseURL провайдера, либо
// "" когда пин не нужен: localhost и IP-литералы резолвить не требуется.
// Сплит-DNS корпоративного VPN не виден Docker VM: имя корпоративного
// LLM-шлюза внутри контейнера не резолвится (ENOTFOUND), хотя сам IP
// доступен, — поэтому резолвим на хосте в момент создания контейнера.
// Из ответов предпочитаем IPv4.
func pinnedHost(baseURL string, lookup func(string) ([]net.IP, error)) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Hostname() == "" {
		return "", nil // нет валидного URL — пинить нечего
	}
	host := u.Hostname()
	if host == "localhost" || net.ParseIP(host) != nil {
		return "", nil
	}
	ips, err := lookup(host)
	if err != nil {
		return "", fmt.Errorf("резолв %s: %w", host, err)
	}
	pick := ""
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			pick = v4.String()
			break
		}
	}
	if pick == "" && len(ips) > 0 {
		pick = ips[0].String()
	}
	if pick == "" {
		return "", fmt.Errorf("резолв %s не дал адресов", host)
	}
	return host + ":" + pick, nil
}

// pinsForBaseURLs adds one DNS pin per distinct hostname, preserving URL order.
// Lookup failures are returned for logging while other endpoints still pin.
func pinsForBaseURLs(baseURLs []string, lookup func(string) ([]net.IP, error)) ([]string, []error) {
	seenHosts := map[string]bool{}
	var pins []string
	var errs []error
	for _, baseURL := range baseURLs {
		u, err := url.Parse(baseURL)
		if err != nil || u.Hostname() == "" {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if seenHosts[host] {
			continue
		}
		seenHosts[host] = true
		pin, err := pinnedHost(baseURL, lookup)
		if err != nil {
			errs = append(errs, err)
		} else if pin != "" {
			pins = append(pins, pin)
		}
	}
	return pins, errs
}

// removeByName удаляет контейнер по имени (чистка «хвостов» рестартов).
func (rt *Runtime) removeByName(ctx context.Context, name string) error {
	list, err := rt.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return err
	}
	for _, c := range list.Items {
		for _, n := range c.Names {
			if strings.TrimPrefix(n, "/") == name {
				_, err := rt.cli.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true})
				return err
			}
		}
	}
	return nil
}

// Remove убивает и удаляет контейнер.
func (rt *Runtime) Remove(ctx context.Context, containerID string) error {
	_, err := rt.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true})
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
}

// Logs — журнал контейнера (для кнопки «Логи»).
// HealthStatus возвращает Docker-health контейнера ("healthy"/"unhealthy"/
// "starting"); пустая строка — healthcheck в образе не задан. Нужен reconcile:
// «процесс жив» и «сервис отвечает» — не одно и то же (зависший dsh web).
func (rt *Runtime) HealthStatus(ctx context.Context, containerID string) (string, error) {
	insp, err := rt.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	if insp.Container.State == nil || insp.Container.State.Health == nil {
		return "", nil
	}
	return string(insp.Container.State.Health.Status), nil
}

// Logs возвращает хвост логов контейнера, убирая multiplexed-заголовки
// демона Docker, если они есть.
func (rt *Runtime) Logs(ctx context.Context, containerID string, tailN string) (string, error) {
	res, err := rt.cli.ContainerLogs(ctx, containerID, client.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Tail: tailN,
	})
	if err != nil {
		return "", err
	}
	defer res.Close()
	b, err := io.ReadAll(res)
	if err != nil {
		return "", err
	}
	return stripDockerStream(b), nil
}

// HasActiveSSH checks Docker's process snapshot only for developer containers.
// sshd's master/listener is baseline; a per-connection `sshd: user@...` child
// means the developer is actively using the runtime through SSH.
func (rt *Runtime) HasActiveSSH(ctx context.Context, handle agctlrt.ChatHandle) (bool, error) {
	if handle.SSHPort == 0 || handle.InstanceID == "" {
		return false, nil
	}
	top, err := rt.cli.ContainerTop(ctx, handle.InstanceID, client.ContainerTopOptions{Arguments: []string{"-eo", "pid,args"}})
	if err != nil {
		return false, err
	}
	return hasActiveSSHProcess(top.Processes), nil
}

// hasActiveSSHProcess — true, если среди процессов контейнера есть живой
// sshd-сеанс (не [listener]): полная команда в последней колонке ps.
func hasActiveSSHProcess(processes [][]string) bool {
	for _, row := range processes {
		if len(row) == 0 {
			continue
		}
		// ContainerTop's `pid,args` result keeps the full ps command in its last
		// column; preceding cells (PID) are intentionally ignored.
		cmd := strings.TrimSpace(strings.ToLower(row[len(row)-1]))
		if strings.HasPrefix(cmd, "sshd:") && !strings.Contains(cmd, "[listener]") {
			return true
		}
	}
	return false
}

// stripDockerStream убирает multiplexed-заголовки демона, если они есть.
func stripDockerStream(b []byte) string {
	if len(b) > 8 && b[0] <= 2 {
		var out bytes.Buffer
		r := bytes.NewReader(b)
		for {
			hdr := make([]byte, 8)
			if _, err := io.ReadFull(r, hdr); err != nil {
				break
			}
			size := int64(uint32(hdr[4])<<24 | uint32(hdr[5])<<16 | uint32(hdr[6])<<8 | uint32(hdr[7]))
			if _, err := io.CopyN(&out, r, size); err != nil {
				break
			}
		}
		return out.String()
	}
	return string(b)
}

// isNotFound распознаёт ошибки отсутствия контейнера/ресурса Docker по
// тексту (Docker API не даёт типизированной ошибки).
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "No such") || strings.Contains(err.Error(), "not found")
}

// FreePort — свободный TCP-порт на loopback (назначается ДО create).
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// containerEnv — env контейнера рабочего места. Ключей провайдеров LLM здесь
// нет намеренно (карточка local-llm-key-container-env): в env их видно в
// docker inspect и /proc/*/environ, их наследуют MCP-серверы, git-хуки и
// npm-скрипты агента. dsh читает ключи из DSH_HOME/.credentials.yaml. Файл
// агент технически прочитать может (его процессы — тот же пользователь), это
// задокументированное ограничение dsh-credentials-local.
// overrideEnv — env с заменой одноимённых переменных из extra (KEY=VALUE).
func overrideEnv(env, extra []string) []string {
	if len(extra) == 0 {
		return env
	}
	keys := map[string]bool{}
	for _, kv := range extra {
		if i := strings.IndexByte(kv, '='); i > 0 {
			keys[kv[:i]] = true
		}
	}
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		if i := strings.IndexByte(kv, '='); i > 0 && keys[kv[:i]] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// reservedContainerPaths — каталоги образа, поверх которых (и внутри
// которых) папку хоста по её пути не монтируем (режим «пути как на хосте»).
var reservedContainerPaths = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/proc", "/root", "/run", "/sbin",
	"/sys", "/usr", "/workspace", "/home/dsh",
	"/opt/dsh", "/opt/dsh-skel", "/opt/npm-mcp", "/opt/ms-playwright",
	"/tmp/npm-cache", "/tmp/cache",
	"/var/lib", "/var/run", "/var/cache", "/var/log", "/var/spool", "/var/tmp",
}

// reservedContainerRoots — корни, которые нельзя закрыть папкой целиком
// (подкаталоги — можно: /tmp/proj, /home/user/src, /var/folders/… macOS).
var reservedContainerRoots = []string{"/home", "/media", "/mnt", "/opt", "/srv", "/tmp", "/var"}

// HostPathMountable — папку хоста можно смонтировать в контейнер по тому
// же пути: абсолютный POSIX-путь (не C:\…) вне системных каталогов образа.
func HostPathMountable(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, ":\\") {
		return false
	}
	clean := path.Clean(p)
	if clean == "/" {
		return false
	}
	for _, r := range reservedContainerPaths {
		if clean == r || strings.HasPrefix(clean, r+"/") {
			return false
		}
	}
	for _, r := range reservedContainerRoots {
		if clean == r {
			return false
		}
	}
	return true
}

func containerEnv(wsID string, devEnv []string) []string {
	env := []string{
		"AGCTL_WS_ID=" + wsID,
		// рабочее место агента — /workspace (репка), а не /root;
		// кэши npm/xdg уводим в tmp, чтобы не мусорить в репке
		"HOME=/workspace",
		"NPM_CONFIG_CACHE=/tmp/npm-cache",
		"XDG_CACHE_HOME=/tmp/cache",
	}
	// Доменные переменные developer'а (GitLab/Jira из .env хоста — серверный
	// режим; доступы локального режима — localChatExtras).
	return append(env, devEnv...)
}
