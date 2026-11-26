package provision

// clonedir.go — копия каталога dsh-home для клона рабочего места. На macOS
// (APFS) — клон файлов без копирования данных (cp -c), на Linux — reflink,
// если файловая система умеет (btrfs, XFS), иначе обычная копия; везде
// запасной путь — копия средствами Go (права, симлинки, пустые каталоги).

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// CloneDir копирует каталог src в dst (dst не должен существовать; его
// родитель создаётся). Возвращает способ: "clonefile", "reflink" или "copy".
func CloneDir(src, dst string) (string, error) {
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("клон: %s — не каталог", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("клон: %s уже существует", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	switch runtime.GOOS {
	case "darwin":
		if err := exec.CommandContext(ctx, "cp", "-c", "-R", "-p", src, dst).Run(); err == nil {
			return "clonefile", nil
		}
		_ = os.RemoveAll(dst) // частичная копия неудачного клона
	case "linux":
		if err := exec.CommandContext(ctx, "cp", "-a", "--reflink=auto", src, dst).Run(); err == nil {
			return "reflink", nil
		}
		_ = os.RemoveAll(dst)
	}
	if err := copyTree(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return "copy", nil
}

// copyTree — переносимая копия дерева: каталоги, файлы с правами, симлинки.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		case info.Mode().IsRegular():
			return copyRegular(p, target, info.Mode().Perm())
		default:
			return nil // сокеты, fifo — не копируем
		}
	})
}

func copyRegular(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src) // #nosec G304 — путь из обхода каталога-источника
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
