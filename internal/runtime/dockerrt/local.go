package dockerrt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/client"
)

// Тонкие помощники для CLI локального режима (agctl dev init/update/doctor):
// без Runtime и без контекста сборки.

func localClient() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker: client: %w", err)
	}
	return cli, nil
}

// PingDaemon — доступен ли Docker; возвращает версию API демона.
func PingDaemon(ctx context.Context) (string, error) {
	cli, err := localClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ping, err := cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		return "", fmt.Errorf("docker недоступен (запустите Docker Desktop / dockerd): %w", err)
	}
	return ping.APIVersion, nil
}

// DaemonArch — архитектура демона Docker (arm64|amd64; "" — не узнали):
// под неё сборка берёт guest-бинарь.
func DaemonArch(ctx context.Context) string {
	cli, err := localClient()
	if err != nil {
		return ""
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return ""
	}
	return goArch(res.Info.Architecture)
}

// ImageID — id локального образа ref ("" и ошибка, если его нет).
func ImageID(ctx context.Context, ref string) (string, error) {
	cli, err := localClient()
	if err != nil {
		return "", err
	}
	defer cli.Close()
	insp, err := cli.ImageInspect(ctx, ref)
	if err != nil {
		return "", err
	}
	return insp.ID, nil
}

// RemoveImage — удалить тег образа ref из локального Docker. Слои уходят,
// когда на них не ссылается ни один тег; образ контейнера (даже
// остановленного) Docker удалить не даст — это ошибка, а не принуждение.
func RemoveImage(ctx context.Context, ref string) error {
	cli, err := localClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.ImageRemove(ctx, ref, client.ImageRemoveOptions{PruneChildren: true})
	return err
}

// ImageMeta — что лаунчер показывает об образе: метки (io.agctl.* —
// версия dsh, сборка и уровень API guest, вид образа), дата и размер.
type ImageMeta struct {
	ID      string
	Labels  map[string]string
	Created string // время сборки по Docker (RFC 3339)
	Size    int64  // байт
}

// ImageInfo — метаданные локального образа ref (ошибка, если его нет).
func ImageInfo(ctx context.Context, ref string) (ImageMeta, error) {
	cli, err := localClient()
	if err != nil {
		return ImageMeta{}, err
	}
	defer cli.Close()
	insp, err := cli.ImageInspect(ctx, ref)
	if err != nil {
		return ImageMeta{}, err
	}
	m := ImageMeta{ID: insp.ID, Created: insp.Created, Size: insp.Size}
	if insp.Config != nil {
		m.Labels = insp.Config.Labels
	}
	return m, nil
}

// PullMissing — реестр ответил, что образа нет или он закрыт: manifest
// unknown, not found, repository does not exist, denied/unauthorized (так
// GHCR отвечает и на ещё не опубликованный или приватный пакет). Сбой связи
// (DNS, VPN, прокси, таймаут) и отмена — не сюда: собирать образ на месте
// под именем реестра из-за сбоя связи нельзя — потом он уже не скачается.
func PullMissing(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"manifest unknown", "not found", "does not exist", "denied", "unauthorized", "no such image"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// PullImage тянет ref из реестра (учётные данные — docker login на хосте).
func PullImage(ctx context.Context, ref string) error {
	cli, err := localClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	pr, err := cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}
	defer pr.Close()
	if err := pr.Wait(ctx); err != nil {
		return fmt.Errorf("pull %s: %w", ref, err)
	}
	if _, err := cli.ImageInspect(ctx, ref); err != nil {
		return fmt.Errorf("pull %s: образ не появился: %w", ref, err)
	}
	return nil
}
