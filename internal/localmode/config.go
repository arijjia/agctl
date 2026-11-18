// Package localmode — локальный режим agctl (agctl dev): однопользовательская
// платформа на машине разработчика. Здесь — конфиг dev.yaml, секреты
// (хранилище ОС), файл состояния запущенного процесса и бэкапы dsh-home.
// Сам сервер — обычный internal/server с Config.Local.
package localmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arijjia/agctl/internal/localbridge"
)

// DefaultAddr — адрес локального UI (не совпадает с серверным 8929, чтобы
// оба режима уживались на одной машине).
const DefaultAddr = "127.0.0.1:8930"

// Home — дом локального режима: $AGCTL_DEV_HOME или ~/.agctl-dev. Отдельно от
// серверного ~/.agctl: данные режимов не смешиваются.
func Home() string {
	if h := strings.TrimSpace(os.Getenv("AGCTL_DEV_HOME")); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".agctl-dev")
	}
	return ".agctl-dev"
}

// Config — ~/.agctl-dev/dev.yaml. Секретов здесь нет: только имена.
type Config struct {
	Profile    string  `yaml:"profile,omitempty"`     // id профиля (agctl, utmn…)
	ProfileDir string  `yaml:"profile_dir,omitempty"` // каталог профиля с диска
	Image      string  `yaml:"image,omitempty"`       // образ рабочего места
	Flavor     string  `yaml:"flavor,omitempty"`      // вкус образа (web, 1c…) — для справки
	WorkDir    string  `yaml:"workdir,omitempty"`     // папка проектов → /workspace
	Addr       string  `yaml:"addr,omitempty"`        // адрес UI (loopback)
	MemGB      float64 `yaml:"mem_gb,omitempty"`
	CPUs       float64 `yaml:"cpus,omitempty"`
	// Env — НЕсекретные переменные контейнера (URL Jira/GitLab, логины).
	Env map[string]string `yaml:"env,omitempty"`
	// Secrets — имена секретов в хранилище ОС (значения там, не здесь).
	Secrets []string `yaml:"secrets,omitempty"`
	Bridge  struct {
		Tools []localbridge.Tool `yaml:"tools,omitempty"`
	} `yaml:"bridge,omitempty"`
	// PreviousImage — образ до последнего update (для rollback).
	PreviousImage string `yaml:"previous_image,omitempty"`
}

// Path — путь dev.yaml в доме.
func Path(home string) string { return filepath.Join(home, "dev.yaml") }

// ErrNotInitialized — dev.yaml ещё нет (нужен agctl dev init).
var ErrNotInitialized = errors.New("локальный режим не настроен — выполните agctl dev init")

// Load читает dev.yaml; отсутствие файла — ErrNotInitialized.
func Load(home string) (*Config, error) {
	raw, err := os.ReadFile(Path(home))
	if os.IsNotExist(err) {
		return nil, ErrNotInitialized
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("dev.yaml: %w", err)
	}
	c.defaults()
	if err := localbridge.Validate(c.Bridge.Tools); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) defaults() {
	if c.Addr == "" {
		c.Addr = DefaultAddr
	}
	if c.Profile == "" && c.ProfileDir == "" {
		c.Profile = "agctl"
	}
	if c.MemGB == 0 {
		c.MemGB = 4
	}
	if c.CPUs == 0 {
		c.CPUs = float64(min(4, runtime.NumCPU()))
	}
}

// Save пишет dev.yaml атомарно (0600: там пути и URL, не секреты, но
// конфиг личный).
func (c *Config) Save(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	raw, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	head := []byte("# agctl dev — локальный режим. Секретов здесь нет: значения в хранилище ОС\n# (agctl dev secrets). Белый список моста к хосту — bridge.tools.\n")
	tmp := Path(home) + ".tmp"
	if err := os.WriteFile(tmp, append(head, raw...), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path(home))
}

// ValidateWorkDir — папка проектов: существующий абсолютный каталог.
// Домашний каталог целиком и корень ФС запрещены (секреты пользователя в
// досягаемости агента, медленный маунт на macOS).
func ValidateWorkDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("папка проектов не задана (agctl dev up DIR)")
	}
	abs, err := filepath.Abs(expandHome(dir))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("папка проектов %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("папка проектов %s — не каталог", abs)
	}
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return "", errors.New("корень файловой системы нельзя отдавать агенту целиком")
	}
	if h, err := os.UserHomeDir(); err == nil {
		if hr, err := filepath.EvalSymlinks(h); err == nil {
			h = hr
		}
		if filepath.Clean(abs) == filepath.Clean(h) {
			return "", errors.New("домашний каталог целиком отдавать агенту нельзя (там ~/.ssh, токены) — укажите папку проектов, например ~/src")
		}
	}
	return abs, nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[1:])
		}
	}
	return p
}
