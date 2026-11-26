package localmode

// layout.go — единственный источник путей локального режима. Всё лежит под
// одним корнем (карточка host-layout):
//
//	~/.agctl/                      корень; переопределяется AGCTL_ROOT
//	  bin/agctl, bin/agctl-server  бинари
//	  bin/guest/agctl-guest[-server]-linux-<arch>   guest для сборки образа
//	  profiles/<id>/               профиль организации — единственный экземпляр
//	  homes/<id>/                  дом: config.yaml, data/, logs/, run/, backups/
//
// На Windows тот же вид: %USERPROFILE%\.agctl (os.UserHomeDir).
// Скрипты установки (bin/install.sh, bin/install.ps1, bin/install.mk,
// bin/local.sh) зеркалят эту раскладку — менять синхронно.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultHomeID — дом по умолчанию (нейтральный профиль agctl).
const DefaultHomeID = "agctl"

// ConfigName и LegacyConfigName — конфиг дома и его прежнее имя (читается,
// при первой записи заменяется на ConfigName).
const (
	ConfigName       = "config.yaml"
	LegacyConfigName = "dev.yaml"
)

// Root — корень локального режима: $AGCTL_ROOT или ~/.agctl.
func Root() string {
	if r := strings.TrimSpace(os.Getenv("AGCTL_ROOT")); r != "" {
		if abs, err := filepath.Abs(r); err == nil {
			return abs
		}
		return r
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".agctl")
	}
	return ".agctl"
}

// RootSource — откуда взят корень (для agctl dev paths).
func RootSource() string {
	if strings.TrimSpace(os.Getenv("AGCTL_ROOT")) != "" {
		return "переменная AGCTL_ROOT"
	}
	return "по умолчанию (AGCTL_ROOT не задан)"
}

// BinDir — бинари agctl и agctl-server.
func BinDir() string { return filepath.Join(Root(), "bin") }

// GuestDir — linux-сборки agctl-guest[-server]-linux-<arch> для сборки образа
// рабочего места на этой машине.
func GuestDir() string { return filepath.Join(BinDir(), "guest") }

// ProfilesDir — профили организаций: <корень>/profiles/<id> (клон,
// распакованный комплект или симлинк на репо при работе из исходников).
func ProfilesDir() string { return filepath.Join(Root(), "profiles") }

// HomesDir — дома локального режима.
func HomesDir() string { return filepath.Join(Root(), "homes") }

// HomeDir — дом с данным id.
func HomeDir(id string) string { return filepath.Join(HomesDir(), id) }

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ValidID — id дома или профиля: строчные латинские буквы, цифры, «.», «_»,
// «-»; с буквы или цифры (безопасен как имя каталога и часть имени сервиса
// хранилища ОС).
func ValidID(id string) bool { return idRe.MatchString(id) && !strings.Contains(id, "..") }

// Home — выбранный дом локального режима.
type Home struct {
	ID     string // id дома (имя каталога в homes/; у AGCTL_DEV_HOME — см. HomeID)
	Dir    string // абсолютный путь дома
	Source string // чем выбран: флаг, переменная, единственный дом, по умолчанию
}

// Источники выбора дома (Home.Source).
const (
	HomeFromFlag    = "флаг --home"
	HomeFromDevHome = "переменная AGCTL_DEV_HOME (путь)"
	HomeFromEnvID   = "переменная AGCTL_HOME_ID"
	HomeFromSingle  = "единственный дом в homes/"
	HomeFromDefault = "по умолчанию"
)

// SelectHome выбирает дом: флаг --home <id> > AGCTL_DEV_HOME (абсолютный путь
// дома — переопределение для тестов и e2e) > AGCTL_HOME_ID > единственный
// существующий дом > agctl.
func SelectHome(flagID string) (Home, error) {
	if id := strings.TrimSpace(flagID); id != "" {
		if !ValidID(id) {
			return Home{}, fmt.Errorf("--home %q: id дома — строчные латинские буквы, цифры, «.», «_», «-»", id)
		}
		return Home{ID: id, Dir: HomeDir(id), Source: HomeFromFlag}, nil
	}
	if dir := strings.TrimSpace(os.Getenv("AGCTL_DEV_HOME")); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return Home{}, fmt.Errorf("AGCTL_DEV_HOME %q: %w", dir, err)
		}
		return Home{ID: HomeID(abs), Dir: abs, Source: HomeFromDevHome}, nil
	}
	if id := strings.TrimSpace(os.Getenv("AGCTL_HOME_ID")); id != "" {
		if !ValidID(id) {
			return Home{}, fmt.Errorf("AGCTL_HOME_ID %q: id дома — строчные латинские буквы, цифры, «.», «_», «-»", id)
		}
		return Home{ID: id, Dir: HomeDir(id), Source: HomeFromEnvID}, nil
	}
	if ids := ListHomes(); len(ids) == 1 {
		return Home{ID: ids[0], Dir: HomeDir(ids[0]), Source: HomeFromSingle}, nil
	}
	return Home{ID: DefaultHomeID, Dir: HomeDir(DefaultHomeID), Source: HomeFromDefault}, nil
}

// ListHomes — id настроенных домов (каталоги homes/<id> с конфигом), по
// алфавиту.
func ListHomes() []string {
	entries, err := os.ReadDir(HomesDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !ValidID(e.Name()) {
			continue
		}
		if _, ok := existingConfig(HomeDir(e.Name())); ok {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// HomeID — id дома по его каталогу: имя каталога в homes/; для дома вне корня
// (AGCTL_DEV_HOME) — AGCTL_HOME_ID, иначе имя каталога, приведённое к
// допустимому id. От id зависит имя сервиса секретов в хранилище ОС.
func HomeID(dir string) string {
	dir = filepath.Clean(dir)
	base := filepath.Base(dir)
	if filepath.Dir(dir) == filepath.Clean(HomesDir()) && ValidID(base) {
		return base
	}
	if id := strings.TrimSpace(os.Getenv("AGCTL_HOME_ID")); ValidID(id) {
		return id
	}
	id := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return '-'
	}, base)
	id = strings.TrimLeft(id, "._-")
	if len(id) > 63 {
		id = id[:63]
	}
	if !ValidID(id) {
		return "custom"
	}
	return id
}

// existingConfig — путь существующего конфига дома: config.yaml, иначе
// прежний dev.yaml.
func existingConfig(home string) (string, bool) {
	for _, name := range []string{ConfigName, LegacyConfigName} {
		p := filepath.Join(home, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}
