package dockerrt

import (
	"context"
	"fmt"
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
