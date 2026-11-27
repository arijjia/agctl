package localmode

// profile.go — единое разрешение профиля локального режима (одна функция для
// CLI и для serve внутри agctl dev) и то, что читается из профиля на каждом
// старте: белый список моста. Из профиля в дом ничего не копируется.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/profile"
)

// Откуда разрешён профиль (ProfileRef.Origin).
const (
	ProfileFromConfig   = "config.yaml: profile_dir"
	ProfileFromRoot     = "<корень>/profiles/<id>"
	ProfileFromEmbedded = "встроен в бинарь"
)

// ProfileRef — где лежит разрешённый профиль.
type ProfileRef struct {
	ID     string `json:"id"`
	Dir    string `json:"dir,omitempty"`  // каталог на диске; пусто — встроенный
	Link   string `json:"link,omitempty"` // цель симлинка, если Dir — симлинк (работа из исходников)
	Origin string `json:"origin"`         // чем разрешён
}

// ResolveProfile — порядок разрешения профиля локального режима: явный
// profile_dir из config.yaml > <корень>/profiles/<id> > встроенный в бинарь.
// Никаких путей относительно текущего каталога.
func ResolveProfile(cfg *Config) (*profile.Data, ProfileRef, error) {
	if dir := strings.TrimSpace(cfg.ProfileDir); dir != "" {
		p, err := profile.LoadDir(dir)
		if err != nil {
			return nil, ProfileRef{}, err
		}
		abs, _ := filepath.Abs(dir)
		return p, ProfileRef{ID: p.ID, Dir: abs, Link: linkTarget(abs), Origin: ProfileFromConfig}, nil
	}
	id := strings.TrimSpace(cfg.Profile)
	if id == "" {
		id = "agctl"
	}
	if !ValidID(id) {
		return nil, ProfileRef{}, fmt.Errorf("профиль %q: id — строчные латинские буквы, цифры, «.», «_», «-»", id)
	}
	dir := filepath.Join(ProfilesDir(), id)
	if _, err := os.Stat(filepath.Join(dir, "profile.yaml")); err != nil {
		if v, ok := profileVariants(ProfilesDir())[id]; ok {
			dir = v // вариант внутри каталога базового профиля (org/dev → org-dev)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "profile.yaml")); err == nil {
		p, err := profile.LoadDir(dir)
		if err != nil {
			return nil, ProfileRef{}, err
		}
		return p, ProfileRef{ID: p.ID, Dir: dir, Link: linkTarget(dir), Origin: ProfileFromRoot}, nil
	}
	if p, err := profile.Embedded(id); err == nil {
		return p, ProfileRef{ID: p.ID, Origin: ProfileFromEmbedded}, nil
	}
	return nil, ProfileRef{}, fmt.Errorf("профиль %q не найден: нет %s; встроенные: %s; профили в %s: %s (свой каталог — agctl dev init --profile-dir)",
		id, filepath.Join(dir, "profile.yaml"), strings.Join(profile.EmbeddedNames(), ", "),
		ProfilesDir(), orDash(strings.Join(DiskProfiles(), ", ")))
}

// DiskProfiles — id профилей в <корень>/profiles: каталоги с profile.yaml и
// варианты во вложенных каталогах (profileVariants).
func DiskProfiles() []string {
	entries, err := os.ReadDir(ProfilesDir())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(ProfilesDir(), e.Name(), "profile.yaml")); err == nil {
			seen[e.Name()] = true
			out = append(out, e.Name())
		}
	}
	for id := range profileVariants(ProfilesDir()) {
		if !seen[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// profileVariants — варианты профилей во вложенных каталогах:
// <профили>/<базовый>/<вариант>/profile.yaml (например org/dev с id org-dev
// и extends: org) — так вариант живёт в одном репозитории с базовым и
// едет на другую машину вместе с ним. id → каталог; первый найденный.
func profileVariants(root string) map[string]string {
	out := map[string]string{}
	bases, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, b := range bases {
		subs, err := os.ReadDir(filepath.Join(root, b.Name())) // симлинк на каталог — тоже
		if err != nil {
			continue
		}
		for _, s := range subs {
			dir := filepath.Join(root, b.Name(), s.Name())
			raw, err := os.ReadFile(filepath.Join(dir, "profile.yaml"))
			if err != nil {
				continue
			}
			var head struct {
				ID string `yaml:"id"`
			}
			if yaml.Unmarshal(raw, &head) != nil || !ValidID(head.ID) {
				continue
			}
			if _, dup := out[head.ID]; !dup {
				out[head.ID] = dir
			}
		}
	}
	return out
}

func linkTarget(path string) string {
	if st, err := os.Lstat(path); err != nil || st.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ProfileBridgeTools — белый список моста профиля организации
// (local/bridge-tools.yaml, ключ tools). Нет файла — пусто.
func ProfileBridgeTools(p *profile.Data) ([]localbridge.Tool, error) {
	if p == nil {
		return nil, nil
	}
	fsys := p.View().FS
	if fsys == nil {
		return nil, nil
	}
	raw, err := fs.ReadFile(fsys, "local/bridge-tools.yaml")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		Tools []localbridge.Tool `yaml:"tools"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("local/bridge-tools.yaml: %w", err)
	}
	if err := localbridge.Validate(doc.Tools); err != nil {
		return nil, fmt.Errorf("local/bridge-tools.yaml: %w", err)
	}
	return doc.Tools, nil
}

// BridgeTool — инструмент моста с источником (для agctl dev bridge list).
type BridgeTool struct {
	localbridge.Tool
	Personal bool // true — из config.yaml дома; false — из профиля
}

// MergeBridgeTools — действующий белый список моста: инструменты профиля в
// их порядке, затем личные из config.yaml; личный с тем же именем заменяет
// профильный на его месте (владелец машины сильнее профиля).
func MergeBridgeTools(fromProfile, personal []localbridge.Tool) []BridgeTool {
	out := make([]BridgeTool, 0, len(fromProfile)+len(personal))
	index := map[string]int{}
	for _, t := range fromProfile {
		index[t.Name] = len(out)
		out = append(out, BridgeTool{Tool: t})
	}
	for _, t := range personal {
		if i, ok := index[t.Name]; ok {
			out[i] = BridgeTool{Tool: t, Personal: true}
			continue
		}
		index[t.Name] = len(out)
		out = append(out, BridgeTool{Tool: t, Personal: true})
	}
	return out
}

// EffectiveBridgeTools — белый список моста для запуска: профиль (читается
// сейчас, с диска) + личные из config.yaml.
func EffectiveBridgeTools(cfg *Config, p *profile.Data) ([]localbridge.Tool, error) {
	fromProfile, err := ProfileBridgeTools(p)
	if err != nil {
		return nil, err
	}
	merged := MergeBridgeTools(fromProfile, cfg.Bridge.Tools)
	out := make([]localbridge.Tool, 0, len(merged))
	for _, t := range merged {
		out = append(out, t.Tool)
	}
	if err := localbridge.Validate(out); err != nil {
		return nil, err
	}
	return out, nil
}
