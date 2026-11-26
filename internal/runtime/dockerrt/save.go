package dockerrt

import (
	"context"
	"fmt"
	"io"
	"os"
)

// SaveImage выгружает локальный образ ref в tar-файл path (как docker save):
// рантайм sbx держит шаблоны в собственном хранилище образов и принимает их
// файлом (sbx template load).
func SaveImage(ctx context.Context, ref, path string) error {
	cli, err := localClient()
	if err != nil {
		return err
	}
	defer cli.Close()
	rc, err := cli.ImageSave(ctx, []string{ref})
	if err != nil {
		return fmt.Errorf("save %s: %w", ref, err)
	}
	defer rc.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return fmt.Errorf("save %s: %w", ref, err)
	}
	return f.Close()
}
