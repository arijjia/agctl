// Package dockerrt — реализация Runtime над Docker Engine API (нативный
// клиент moby/moby/client: unix socket на mac/linux, named pipe на Windows).
// Образ agctl-dsh строится самим agctl из встроенного контекста (Dockerfile,
// relay, скелет DSH_HOME) + linux-сборки агctl (dist/agctl-linux-<arch>).
package dockerrt

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/arijjia/agctl/internal/provision"
)

// Контекст образа приходит из internal/assets (fs.FS с префиксом "image").

// Config — параметры рантайма.
type Config struct {
	Image       string // agctl-dsh:<tag>
	MemoryBytes int64  // лимит памяти контейнера
	NanoCPUs    int64  // лимит CPU
	BinDir      string // каталог с linux-сборками agctl (dist/)
}

// Runtime — docker-реализация.
type Runtime struct {
	cli     *client.Client
	cfg     Config
	imageFS fs.FS // контекст образа (префикс "image")
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
		cfg.Image = "agctl-dsh:0.1.0"
	}
	_ = ping
	return &Runtime{cli: cli, cfg: cfg, imageFS: imageFS}, nil
}

// Close освобождает клиент.
func (rt *Runtime) Close() error { return rt.cli.Close() }

// ── образ ────────────────────────────────────────────────────────────────────

// EnsureImage строит образ agctl-dsh, если его нет. Требует linux-сборку
// агctl в cfg.BinDir (её делает `make image` / `make release-local`).
func (rt *Runtime) EnsureImage(ctx context.Context) error {
	if _, err := rt.cli.ImageInspect(ctx, rt.cfg.Image); err == nil {
		return nil
	}
	bin, err := findLinuxBinary(rt.cfg.BinDir)
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
	dst := filepath.Join(ctxDir, "agctl")
	if err := copyFile(bin, dst); err != nil {
		return fmt.Errorf("docker: копирование %s: %w", bin, err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tarDir(ctxDir, tw); err != nil {
		return fmt.Errorf("docker: tar: %w", err)
	}
	if err := tw.Close(); err != nil {
		return err
	}
	res, err := rt.cli.ImageBuild(ctx, &buf, client.ImageBuildOptions{
		Tags:        []string{rt.cfg.Image},
		Dockerfile:  "Dockerfile",
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
	if _, err := rt.cli.ImageInspect(ctx, rt.cfg.Image); err != nil {
		return fmt.Errorf("docker: образ не собрался; вывод:\n%s", tail(string(out), 4000))
	}
	return nil
}

func findLinuxBinary(binDir string) (string, error) {
	// при запуске на mac собираем под архитектуру демона docker; на практике
	// make image кладёт dist/agctl-linux-<arm64|amd64> — берём обе по очереди.
	for _, arch := range []string{"arm64", "amd64"} {
		p := filepath.Join(binDir, "agctl-linux-"+arch)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if _, err := os.Stat(filepath.Join(binDir, "agctl-linux-"+runtime.GOARCH)); err == nil {
		return filepath.Join(binDir, "agctl-linux-"+runtime.GOARCH), nil
	}
	return "", errors.New("нет linux-сборки agctl: выполните `make image` (она собирает dist/agctl-linux-*)")
}

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
}

// Start создаёт и запускает контейнер воркспейса:
// монтирует рабочий клон и DSH_HOME, публикует порт dsh на 127.0.0.1:hostPort,
// прокидывает LLM-токен через env (секрет не пишется в образ/логи).
func (rt *Runtime) Start(ctx context.Context, ws provision.Layout, wsID string, hostPort int, p provision.Provider) (StartResult, error) {
	if err := rt.EnsureImage(ctx); err != nil {
		return StartResult{}, err
	}
	// гонки за порт: docker сам отдаст ошибку, если порт занят.
	cfg := client.ContainerCreateOptions{
		Name: "agctl-" + wsID,
		Config: &container.Config{
			Image: rt.cfg.Image,
			Env: []string{
				"AGCTL_LLM_API_KEY=" + p.APIKey,
				"AGCTL_WS_ID=" + wsID,
				// рабочее место агента — /workspace (репка), а не /root;
				// кэши npm/xdg уводим в tmp, чтобы не мусорить в репке
				"HOME=/workspace",
				"NPM_CONFIG_CACHE=/tmp/npm-cache",
				"XDG_CACHE_HOME=/tmp/cache",
			},
			Labels: map[string]string{"agctl.workspace": wsID},
		},
		HostConfig: &container.HostConfig{
			Binds: []string{
				ws.Work + ":/workspace",
				ws.DshHome + ":/home/dsh",
			},
			// host-gateway: из контейнера должен резолвиться
			// host.docker.internal (корпоративный LLM-шлюз на хосте).
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
			PortBindings: network.PortMap{
				network.MustParsePort("3080/tcp"): []network.PortBinding{{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: fmt.Sprint(hostPort)}},
			},
			Resources: container.Resources{
				Memory:   rt.cfg.MemoryBytes,
				NanoCPUs: rt.cfg.NanoCPUs,
			},
			RestartPolicy: container.RestartPolicy{Name: "no"},
		},
	}
	res, err := rt.cli.ContainerCreate(ctx, cfg)
	if err != nil {
		// контейнер с таким именем мог остаться от прошлого прогона — уберём и повторим
		if strings.Contains(err.Error(), "already in use") {
			_ = rt.removeByName(ctx, "agctl-"+wsID)
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
	return StartResult{ContainerID: res.ID, HostPort: hostPort}, nil
}

// Resume запускает существующий (остановленный) контейнер.
func (rt *Runtime) Resume(ctx context.Context, containerID string) error {
	_, err := rt.cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{})
	return err
}

// Stop останавливает контейнер воркспейса (данные живут на хосте).
func (rt *Runtime) Stop(ctx context.Context, containerID string) error {
	timeout := 10
	_, err := rt.cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil && isNotFound(err) {
		return nil
	}
	return err
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
