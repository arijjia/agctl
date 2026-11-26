// Package localmode — локальный режим agctl (agctl dev): однопользовательская
// платформа на машине разработчика. Здесь — раскладка путей (layout.go),
// конфиг дома config.yaml, разрешение профиля, секреты (хранилище ОС), файл
// состояния запущенного процесса и бэкапы dsh-home. Сам сервер — обычный
// internal/server с Config.Local.
package localmode

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arijjia/agctl/internal/localbridge"
)

// DefaultAddr — адрес локального UI (не совпадает с серверным 8929 и
// демо-стендом 8928, чтобы все уживались на одной машине).
const DefaultAddr = "127.0.0.1:8930"

// Config — <дом>/config.yaml. Секретов здесь нет: только имена. Из профиля в
// конфиг ничего не копируется: образ по умолчанию и белый список моста
// профиля читаются из профиля на каждом старте.
type Config struct {
	Profile    string  `yaml:"profile,omitempty"`     // id профиля (agctl или id профиля организации)
	ProfileDir string  `yaml:"profile_dir,omitempty"` // явный каталог профиля
	Image      string  `yaml:"image,omitempty"`       // образ — только явный выбор; пусто = по профилю
	Flavor     string  `yaml:"flavor,omitempty"`      // вкус образа (web, …)
	WorkDir    string  `yaml:"workdir,omitempty"`     // папка проектов → /workspace
	Addr       string  `yaml:"addr,omitempty"`        // адрес UI (loopback)
	MemGB      float64 `yaml:"mem_gb,omitempty"`
	CPUs       float64 `yaml:"cpus,omitempty"`
	// EgressAllowlist — контейнер на выделенной сети: наружу только DNS,
	// хост (мост, LiteLLM на хосте), провайдеры модели, egress.allow профиля и
	// EgressAllow (карточка dockerrt-egress-flag-key-proxy). По умолчанию выкл.
	EgressAllowlist bool     `yaml:"egress_allowlist,omitempty"`
	EgressAllow     []string `yaml:"egress_allow,omitempty"` // хосты, IPv4, CIDR сверх профиля
	// Env — НЕсекретные переменные контейнера (URL Jira/GitLab, логины).
	Env map[string]string `yaml:"env,omitempty"`
	// Secrets — имена секретов в хранилище ОС (значения там, не здесь).
	Secrets []string `yaml:"secrets,omitempty"`
	// Providers — личные провайдеры моделей разработчика поверх провайдеров
	// профиля (лаунчер → «Доступ»). Ключ — в хранилище секретов под именем
	// ProviderKeyEnv(name).
	Providers []Provider `yaml:"providers,omitempty"`
	// Bridge.Tools — ЛИЧНЫЙ белый список моста; объединяется с
	// local/bridge-tools.yaml профиля (MergeBridgeTools).
	Bridge struct {
		Tools []localbridge.Tool `yaml:"tools,omitempty"`
	} `yaml:"bridge,omitempty"`
	// Runtime — рантайм рабочего места: docker (по умолчанию) или sbx
	// (Docker Sandboxes, microVM); см. runtime.go.
	Runtime string `yaml:"runtime,omitempty"`
	// HostPaths — «пути как на хосте»: папки проектов видны в контейнере по
	// тем же путям, что на этой машине (агент, его сессии и предложенные им
	// команды говорят хостовыми путями); /workspace остаётся как был. Только
	// рантайм docker, пути вида /Users/…, /home/…
	HostPaths bool `yaml:"host_paths,omitempty"`
	// PreviousImage — образ до последнего update (для rollback).
	PreviousImage string `yaml:"previous_image,omitempty"`
}

// Path — путь config.yaml в доме (сюда пишет Save).
func Path(home string) string { return filepath.Join(home, ConfigName) }

// LegacyPath — прежний dev.yaml: читается, если config.yaml ещё нет.
func LegacyPath(home string) string { return filepath.Join(home, LegacyConfigName) }

// ErrNotInitialized — конфига дома ещё нет (нужен agctl dev init).
var ErrNotInitialized = errors.New("локальный режим не настроен — выполните agctl dev init")

// Load читает config.yaml (или прежний dev.yaml); нет ни того, ни другого —
// ErrNotInitialized.
func Load(home string) (*Config, error) {
	path, ok := existingConfig(home)
	if !ok {
		return nil, ErrNotInitialized
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	c.defaults()
	if err := localbridge.Validate(c.Bridge.Tools); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return &c, nil
}

func (c *Config) defaults() {
	if len(c.Providers) == 0 {
		c.Providers = nil // «providers: []» в файле и отсутствие поля — одно и то же
	}
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

// Save пишет config.yaml атомарно (0600: там пути и URL, не секреты, но
// конфиг личный). Прежний dev.yaml после первой записи уходит в
// dev.yaml.migrated — источник правды один.
func (c *Config) Save(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	raw, err := c.Render(HomeID(home))
	if err != nil {
		return err
	}
	tmp := Path(home) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, Path(home)); err != nil {
		return err
	}
	if _, err := os.Stat(LegacyPath(home)); err == nil {
		_ = os.Rename(LegacyPath(home), LegacyPath(home)+".migrated")
	}
	return nil
}

// fieldDoc — стандартизированный комментарий к полю config.yaml: что это,
// откуда значение, кто меняет.
type fieldDoc struct {
	key, what, from, who string
}

// configDocs — поля config.yaml в порядке записи. Каждое поле Config обязано
// быть здесь (TestConfigRenderDocumentsEveryField); bridge — последним
// (самый длинный блок, его удобно дописывать в конец).
var configDocs = []fieldDoc{
	{"profile", "профиль организации: id каталога <корень>/profiles/<id>; agctl — нейтральный, встроен в бинарь",
		"agctl dev init --profile <id> или установщик комплекта организации",
		"вы — agctl dev init --profile <id>"},
	{"profile_dir", "явный каталог профиля (сильнее profile); пусто — <корень>/profiles/<id>, затем встроенный",
		"agctl dev init --profile-dir <путь>",
		"вы; обычно пусто"},
	{"image", "образ рабочего места — только ваш явный выбор; пусто — образ по умолчанию из профиля (images.dev)",
		"лаунчер («Образ»), agctl dev init --image, agctl dev update --image",
		"вы через лаунчер или CLI; agctl dev update и rollback"},
	{"flavor", "вкус образа (web, вкус профиля) — по нему образ собирается на этой машине, если его нет в реестре",
		"выводится из выбранного образа или agctl dev init --flavor",
		"agctl вместе с image; вы через --flavor"},
	{"previous_image", "образ до последнего обновления — цель agctl dev rollback",
		"записывает agctl dev update и смена образа в лаунчере",
		"agctl; руками не править"},
	{"runtime", "рантайм рабочего места: docker — контейнер Docker, sbx — Docker Sandboxes (микро-ВМ); пусто — docker",
		"лаунчер (выбор рантайма), agctl dev init|up --runtime docker|sbx",
		"вы через лаунчер или CLI"},
	{"workdir", "папка проектов на этой машине; агент видит только её (в контейнере — /workspace)",
		"agctl dev init --workdir, agctl dev up <папка>, лаунчер",
		"вы"},
	{"host_paths", "true — «пути как на хосте»: папки проектов видны в контейнере ещё и по своим путям на этой машине, агент работает там же (пути в сессиях открываются во внешнем редакторе, команды агента выполняются на хосте как есть); /workspace остаётся. Только рантайм docker, не Windows",
		"лаунчер («Рабочие места» → «пути как на хосте»); по умолчанию false",
		"вы через лаунчер или правку файла; применяется при перезапуске места"},
	{"addr", "адрес веб-интерфейса и моста к хосту; только loopback (127.0.0.1:ПОРТ)",
		"agctl dev init --addr; по умолчанию " + DefaultAddr,
		"вы; применяется после agctl dev down && agctl dev up"},
	{"mem_gb", "лимит памяти контейнера рабочего места, ГБ",
		"по умолчанию 4",
		"вы (правка файла)"},
	{"cpus", "лимит CPU контейнера рабочего места",
		"по умолчанию min(4, число ядер)",
		"вы (правка файла)"},
	{"egress_allowlist", "true — контейнер на выделенной docker-сети: наружу только DNS, хост, провайдеры модели и белый список",
		"по умолчанию false",
		"вы (правка файла)"},
	{"egress_allow", "белый список egress сверх профиля (egress.allow): хосты, IPv4, CIDR; действует при egress_allowlist: true",
		"пусто по умолчанию",
		"вы (правка файла)"},
	{"env", "несекретные переменные контейнера: адреса GitLab/Jira/Confluence, логины",
		"лаунчер («Доступ»), agctl dev secrets set ИМЯ --plain",
		"вы через лаунчер или CLI"},
	{"secrets", "ИМЕНА секретов; значения — в хранилище ОС (сервис agctl:<id дома>), в этом файле их нет",
		"лаунчер («Доступ»), agctl dev secrets set ИМЯ",
		"вы через лаунчер или CLI"},
	{"providers", "ваши личные провайдеры моделей поверх провайдеров профиля: имя, адрес API и модели; ключ каждого — в хранилище секретов под именем AGCTL_PROVIDER_<ИМЯ>_API_KEY",
		"лаунчер («Доступ» → «Добавить провайдера») или настройки моделей в харнессе — правки оттуда переносятся сюда",
		"вы через лаунчер или харнесс"},
	{"bridge", "личный белый список моста к хосту (bridge.tools); объединяется с local/bridge-tools.yaml профиля — тот читается из профиля на каждом старте, одноимённый личный инструмент перекрывает профильный",
		"пусто по умолчанию; формат — docs/architecture.md, раздел 6.6",
		"вы (правка файла); список целиком — agctl dev bridge list"},
}

// values — значения полей в порядке и под ключами configDocs. Пустые
// значения пишутся явно: файл показывает все настройки дома.
func (c *Config) values() map[string]any {
	tools := c.Bridge.Tools
	if tools == nil {
		tools = []localbridge.Tool{}
	}
	env := c.Env
	if env == nil {
		env = map[string]string{}
	}
	secrets := c.Secrets
	if secrets == nil {
		secrets = []string{}
	}
	providers := c.Providers
	if providers == nil {
		providers = []Provider{}
	}
	allow := c.EgressAllow
	if allow == nil {
		allow = []string{}
	}
	return map[string]any{
		"profile": c.Profile, "profile_dir": c.ProfileDir,
		"image": c.Image, "flavor": c.Flavor, "previous_image": c.PreviousImage,
		"workdir": c.WorkDir, "host_paths": c.HostPaths, "addr": c.Addr, "mem_gb": c.MemGB, "cpus": c.CPUs,
		"egress_allowlist": c.EgressAllowlist, "egress_allow": allow,
		"env": env, "secrets": secrets,
		"runtime": c.Runtime, "providers": providers,
		"bridge": map[string]any{"tools": tools},
	}
}

// Render — текст config.yaml: шапка и комментарий к каждому полю.
func (c *Config) Render(homeID string) ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `# agctl — локальный режим, дом «%s».
# Этот файл — настройки дома: <корень>/homes/%s/config.yaml (корень — ~/.agctl
# или AGCTL_ROOT). Его пишут agctl dev init, лаунчер и CLI; править руками
# можно — комментарии при следующей записи восстановятся, значения сохранятся.
# Секретов здесь нет: значения лежат в хранилище ОС (agctl dev secrets).
# Из профиля сюда ничего не копируется: образ по умолчанию и инструменты моста
# профиля читаются из профиля на каждом старте.
# Где что лежит на этой машине — agctl dev paths.
`, homeID, homeID)
	vals := c.values()
	for _, d := range configDocs {
		fmt.Fprintf(&b, "\n# %s — %s.\n#   откуда: %s\n#   меняет: %s\n", d.key, d.what, d.from, d.who)
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		if err := enc.Encode(map[string]any{d.key: vals[d.key]}); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
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
