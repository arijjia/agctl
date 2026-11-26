package main

// dev_migrate.go — `agctl dev migrate`: перенос прежних домов локального
// режима (~/.agctl-dev, ~/.agctl-dev-<id>) в <корень>/homes/<id>. По умолчанию
// только план (dry-run); --apply выполняет. id дома = id профиля.
//
// Что меняется при переносе:
//   - каталог дома переезжает целиком (os.Rename), dev.yaml → config.yaml;
//   - явный profile_dir превращается в ссылку <корень>/profiles/<id> → каталог
//     профиля (профиль — единственный экземпляр под корнем);
//   - из конфига убирается скопированное из профиля: инструменты моста,
//     совпадающие с профильными, и образ, равный образу профиля по умолчанию;
//   - секреты хранилища ОС копируются из общего сервиса agctl-dev в сервис
//     дома agctl:<id> (исходные записи не удаляются);
//   - имена Docker-контейнеров и сети egress содержат sha256 пути data/ —
//     после переноса они другие: контейнеры создадутся заново при первом
//     запуске (данные на маунтах, не в контейнере), старые нужно удалить —
//     команда печатается.

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/server"
)

// migrateItem — план переноса одного прежнего дома.
type migrateItem struct {
	From string // прежний дом
	ID   string // id нового дома (= id профиля)
	To   string // <корень>/homes/<id>
	Skip string // почему не переносится (пусто — переносится)

	ProfileDir  string // явный profile_dir прежнего конфига (абсолютный; пусто — не задан)
	ProfileLink string // ссылка <корень>/profiles/<id>, которую нужно создать (пусто — не нужна)
	Notes       []string
}

func devMigrate(args []string) error {
	fs := flag.NewFlagSet("dev migrate", flag.ExitOnError)
	apply := fs.Bool("apply", false, "выполнить перенос (без флага — только показать план)")
	from := fs.String("from", "", "где искать прежние дома .agctl-dev[-<id>] (по умолчанию — домашний каталог)")
	_ = fs.Parse(args)
	base := *from
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		base = h
	}
	items, err := migratePlan(base)
	if err != nil {
		return err
	}
	return migrateRun(os.Stdout, items, *apply, devHomeRunning)
}

// devHomeRunning — в доме запущен фоновый процесс.
func devHomeRunning(home string) bool {
	st, _ := localmode.ReadState(home)
	return st != nil && devAlive(st)
}

// migratePlan находит прежние дома в base и строит план переноса.
func migratePlan(base string) ([]migrateItem, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.Name() != ".agctl-dev" && !strings.HasPrefix(e.Name(), ".agctl-dev-") {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			continue
		}
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	taken := map[string]string{}
	var items []migrateItem
	for _, dir := range dirs {
		it := migrateItem{From: dir}
		suffix := strings.TrimPrefix(strings.TrimPrefix(filepath.Base(dir), ".agctl-dev"), "-")
		cfg, err := localmode.Load(dir)
		switch {
		case errors.Is(err, localmode.ErrNotInitialized):
			it.Skip = "нет dev.yaml — это не дом локального режима"
		case err != nil:
			it.Skip = "конфиг не читается: " + err.Error()
		default:
			it.ID = cfg.Profile
			if cfg.ProfileDir != "" {
				abs, _ := filepath.Abs(cfg.ProfileDir)
				it.ProfileDir = abs
				if p, err := profile.LoadDir(abs); err == nil {
					it.ID = p.ID
				} else {
					it.ID = suffix
					it.Notes = append(it.Notes, fmt.Sprintf("profile_dir %s не читается (%v) — оставлен в конфиге как есть, id дома взят из имени каталога", abs, err))
					it.ProfileDir = ""
				}
			}
			if it.ID == "" {
				it.ID = localmode.DefaultHomeID
			}
		}
		if it.Skip == "" && !localmode.ValidID(it.ID) {
			it.Skip = fmt.Sprintf("id профиля %q не годится как id дома — перенесите вручную: mv %s %s", it.ID, dir, localmode.HomeDir("<id>"))
		}
		if it.Skip == "" {
			it.To = localmode.HomeDir(it.ID)
			if prev, ok := taken[it.ID]; ok {
				it.Skip = fmt.Sprintf("дом «%s» уже занят переносом %s — два прежних дома с одним профилем; перенесите вручную под другим id: mv %s %s", it.ID, prev, dir, localmode.HomeDir("<id>"))
			} else if _, err := os.Lstat(it.To); err == nil {
				it.Skip = fmt.Sprintf("%s уже существует — перенесите вручную под другим id: mv %s %s", it.To, dir, localmode.HomeDir("<id>"))
			}
		}
		if it.Skip == "" {
			taken[it.ID] = dir
			if it.ProfileDir != "" {
				link := filepath.Join(localmode.ProfilesDir(), it.ID)
				switch target, err := filepath.EvalSymlinks(link); {
				case err != nil && os.IsNotExist(err):
					it.ProfileLink = link
				case err == nil && sameDir(target, it.ProfileDir):
					// профиль уже под корнем — profile_dir просто убирается
				default:
					it.Notes = append(it.Notes, fmt.Sprintf("%s уже есть и указывает не на %s — profile_dir оставлен в конфиге явным", link, it.ProfileDir))
					it.ProfileDir = ""
				}
			}
		}
		items = append(items, it)
	}
	return items, nil
}

func sameDir(a, b string) bool {
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// migrateRun печатает план и, при apply, выполняет его.
func migrateRun(w io.Writer, items []migrateItem, apply bool, running func(string) bool) error {
	fmt.Fprintf(w, "agctl dev migrate — перенос прежних домов в %s\n", localmode.HomesDir())
	if len(items) == 0 {
		fmt.Fprintln(w, "Прежних домов (~/.agctl-dev, ~/.agctl-dev-<id>) нет — переносить нечего.")
		return nil
	}
	if !apply {
		fmt.Fprintln(w, "Это план (dry-run): на диске ничего не меняется. Выполнить — agctl dev migrate --apply")
	}
	failed := 0
	for i := range items {
		it := &items[i]
		fmt.Fprintf(w, "\n%s\n", it.From)
		if it.Skip == "" && running(it.From) {
			it.Skip = "платформа в этом доме запущена — сначала остановите: AGCTL_DEV_HOME=" + it.From + " agctl dev down"
		}
		if it.Skip != "" {
			fmt.Fprintf(w, "  ✗ пропущен: %s\n", it.Skip)
			continue
		}
		oldData, newData := localmode.ServerHome(it.From), localmode.ServerHome(it.To)
		fmt.Fprintf(w, "  → %s  (дом «%s»)\n", it.To, it.ID)
		fmt.Fprintf(w, "  · dev.yaml → config.yaml (с комментариями к полям; прежний файл остаётся как dev.yaml.migrated)\n")
		if it.ProfileLink != "" {
			fmt.Fprintf(w, "  · профиль: ссылка %s → %s, profile_dir из конфига убирается\n", it.ProfileLink, it.ProfileDir)
		} else if it.ProfileDir != "" {
			fmt.Fprintf(w, "  · профиль уже под корнем (%s) — profile_dir из конфига убирается\n", filepath.Join(localmode.ProfilesDir(), it.ID))
		}
		fmt.Fprintf(w, "  · из конфига убирается скопированное из профиля: инструменты моста, равные профильным, и образ, равный образу профиля\n")
		fmt.Fprintf(w, "  · секреты хранилища ОС: копия из общего сервиса «%s» в «%s» (исходные записи остаются)\n", localmode.LegacyKeyringService, localmode.KeyringService(it.ID))
		for _, n := range it.Notes {
			fmt.Fprintf(w, "  ⚠ %s\n", n)
		}
		oldPrefix, newPrefix := server.ChatContainerPrefix(oldData), server.ChatContainerPrefix(newData)
		fmt.Fprintf(w, "  ⚠ Docker: имена зависят от пути data/ и изменятся: контейнеры %s* → %s*, сеть egress %s → %s.\n", oldPrefix, newPrefix, dockerrt.EgressNetworkName(oldData), dockerrt.EgressNetworkName(newData))
		fmt.Fprintf(w, "    Новые контейнеры создадутся при первом agctl dev up (данные на маунтах, не в контейнере). Старые удалите сами:\n")
		fmt.Fprintf(w, "      docker ps -aq --filter name=%s | xargs docker rm -f\n", oldPrefix)
		if !apply {
			continue
		}
		if err := migrateApply(w, it); err != nil {
			failed++
			fmt.Fprintf(w, "  ✗ не перенесён: %v\n", err)
			continue
		}
		fmt.Fprintf(w, "  ✓ перенесён; запуск: agctl dev --home %s up\n", it.ID)
	}
	if failed > 0 {
		return fmt.Errorf("не перенесено домов: %d", failed)
	}
	return nil
}

// migrateApply переносит один дом.
func migrateApply(w io.Writer, it *migrateItem) error {
	if err := os.MkdirAll(filepath.Dir(it.To), 0o700); err != nil {
		return err
	}
	if err := os.Rename(it.From, it.To); err != nil {
		return fmt.Errorf("%w (каталоги на разных разделах? перенесите вручную: mv %s %s — прежний dev.yaml читается и на новом месте)", err, it.From, it.To)
	}
	localmode.ClearState(it.To) // состояние процесса привязано к прежнему пути
	cfg, err := localmode.Load(it.To)
	if err != nil {
		return err
	}
	if it.ProfileDir != "" {
		linked := it.ProfileLink == ""
		if it.ProfileLink != "" {
			if err := os.MkdirAll(filepath.Dir(it.ProfileLink), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(it.ProfileDir, it.ProfileLink); err != nil {
				fmt.Fprintf(w, "  ⚠ ссылка на профиль не создана (%v) — profile_dir оставлен в конфиге явным\n", err)
			} else {
				linked = true
			}
		}
		if linked {
			cfg.Profile, cfg.ProfileDir = it.ID, ""
		}
	}
	if p, _, err := localmode.ResolveProfile(cfg); err == nil {
		if fromProfile, err := localmode.ProfileBridgeTools(p); err == nil {
			cfg.Bridge.Tools = personalTools(cfg.Bridge.Tools, fromProfile)
		}
		if cfg.Image != "" && cfg.Image == devDefaultImage(p, cfg.Flavor) {
			cfg.Image = ""
		}
	} else {
		fmt.Fprintf(w, "  ⚠ профиль не разрешён (%v) — конфиг перенесён без чистки\n", err)
	}
	if err := cfg.Save(it.To); err != nil {
		return err
	}
	names := append([]string{}, cfg.Secrets...)
	for _, v := range localmode.Manifest {
		if v.Secret {
			names = append(names, v.Name)
		}
	}
	if moved, err := localmode.OpenStore(it.To).ImportLegacy(names); err != nil {
		fmt.Fprintf(w, "  ⚠ секреты: %v — значения по-прежнему читаются из общего сервиса «%s»\n", err, localmode.LegacyKeyringService)
	} else if len(moved) > 0 {
		fmt.Fprintf(w, "  · секреты скопированы в сервис дома: %s\n", strings.Join(moved, ", "))
	}
	return nil
}

// personalTools — инструменты моста конфига без тех, что совпадают с
// профильными (их прежний init скопировал из профиля).
func personalTools(tools, fromProfile []localbridge.Tool) []localbridge.Tool {
	byName := map[string]localbridge.Tool{}
	for _, t := range fromProfile {
		byName[t.Name] = t
	}
	var out []localbridge.Tool
	for _, t := range tools {
		if pt, ok := byName[t.Name]; ok && reflect.DeepEqual(pt, t) {
			continue
		}
		out = append(out, t)
	}
	return out
}
