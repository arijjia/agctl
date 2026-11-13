package localmode

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// State — файл состояния запущенного `agctl dev serve` (run/state.json, 0600):
// CLI на хосте по нему находит процесс и управляет им.
type State struct {
	PID          int       `json:"pid"`
	Addr         string    `json:"addr"`
	ControlToken string    `json:"controlToken"`
	LoginToken   string    `json:"loginToken"`
	Image        string    `json:"image"`
	WorkDir      string    `json:"workdir"`
	Started      time.Time `json:"started"`
	Version      string    `json:"version"`
}

// StatePath — путь файла состояния.
func StatePath(home string) string { return filepath.Join(home, "run", "state.json") }

// LogPath — лог фонового процесса.
func LogPath(home string) string { return filepath.Join(home, "logs", "serve.log") }

// BridgeJournalPath — журнал вызовов моста.
func BridgeJournalPath(home string) string { return filepath.Join(home, "logs", "bridge.log") }

// ServerHome — дом данных сервера локального режима (SQLite, dsh-home).
func ServerHome(home string) string { return filepath.Join(home, "data") }

// DshHome — DSH_HOME владельца (сессии, настройки dsh). Владелец локального
// режима — всегда первый пользователь (u1).
func DshHome(home string) string {
	return filepath.Join(ServerHome(home), "chats", "u1", "dsh-home")
}

// WriteState сохраняет состояние (0600).
func WriteState(home string, st State) error {
	dir := filepath.Dir(StatePath(home))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := StatePath(home) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, StatePath(home))
}

// ReadState читает состояние; нет файла — (nil, nil).
func ReadState(home string) (*State, error) {
	raw, err := os.ReadFile(StatePath(home))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ClearState удаляет файл состояния.
func ClearState(home string) { _ = os.Remove(StatePath(home)) }

// ClearOwnState удаляет файл состояния, только если он записан процессом pid
// (упавший на старте второй экземпляр не должен стирать состояние живого).
func ClearOwnState(home string, pid int) {
	if st, err := ReadState(home); err == nil && st != nil && st.PID == pid {
		ClearState(home)
	}
}

// NewToken — случайный токен (32 байта, base64url).
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ── бэкапы dsh-home ─────────────────────────────────────────────────────────

// BackupsDir — каталог бэкапов.
func BackupsDir(home string) string { return filepath.Join(home, "backups") }

// KeepBackups — сколько последних бэкапов хранить.
const KeepBackups = 3

// Backup архивирует dsh-home владельца в backups/dsh-home-<время>-<метка>.tar.gz
// и чистит старые. Пустой/отсутствующий dsh-home — ("", nil).
func Backup(home, label string) (string, error) {
	src := DshHome(home)
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return "", nil
	}
	if err := os.MkdirAll(BackupsDir(home), 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("dsh-home-%s-%s.tar.gz", time.Now().Format("20060102-150405"), sanitizeLabel(label))
	dst := filepath.Join(BackupsDir(home), name)
	f, err := os.OpenFile(dst+".tmp", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	walkErr := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr, err := tar.FileInfoHeader(info, target)
			if err != nil {
				return err
			}
			hdr.Name = filepath.ToSlash(rel)
			return tw.WriteHeader(hdr)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
	closeErr := firstErr(tw.Close(), gz.Close(), f.Close())
	if err := firstErr(walkErr, closeErr); err != nil {
		_ = os.Remove(dst + ".tmp")
		return "", fmt.Errorf("бэкап dsh-home: %w", err)
	}
	if err := os.Rename(dst+".tmp", dst); err != nil {
		return "", err
	}
	pruneBackups(home)
	return dst, nil
}

// Backups — бэкапы, новые первыми.
func Backups(home string) []string {
	entries, _ := os.ReadDir(BackupsDir(home))
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "dsh-home-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			out = append(out, filepath.Join(BackupsDir(home), e.Name()))
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out
}

func pruneBackups(home string) {
	all := Backups(home)
	for i := KeepBackups; i < len(all); i++ {
		_ = os.Remove(all[i])
	}
}

// Restore заменяет dsh-home содержимым архива (текущий уезжает в
// dsh-home.before-restore-<время> — не удаляем данные молча).
func Restore(home, archive string) error {
	dst := DshHome(home)
	tmp := dst + ".restore"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.FromSlash(hdr.Name)
		if !filepath.IsLocal(name) {
			return fmt.Errorf("архив: недопустимый путь %q", hdr.Name)
		}
		path := filepath.Join(tmp, name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, os.FileMode(hdr.Mode)&0o777|0o700); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, dst+".before-restore-"+time.Now().Format("20060102-150405")); err != nil {
			return err
		}
	}
	return os.Rename(tmp, dst)
}

func sanitizeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return '_'
	}, s)
	if len(s) > 60 {
		s = s[:60]
	}
	if s == "" {
		s = "manual"
	}
	return s
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}
