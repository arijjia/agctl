package localmode

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Entry — доступ для экрана «Доступы» (лаунчер) и agctl dev secrets list.
// Значение секрета наружу не отдаётся никогда: только Set.
type Entry struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Hint   string `json:"hint,omitempty"`
	Group  string `json:"group"`  // llm | gitlab | jira | other
	Secret bool   `json:"secret"` // true — хранилище ОС; false — dev.yaml
	Set    bool   `json:"set"`
	Value  string `json:"value,omitempty"` // только у несекретных (URL, логин)
	Known  bool   `json:"known"`           // из манифеста (не удаляется из списка)
}

// editMu — правки dev.yaml из лаунчера и CLI в одном процессе не пересекаются.
var editMu sync.Mutex

// Entries — манифест + всё, что задано в dev.yaml/хранилище, по группам.
func Entries(cfg *Config, store *Store) []Entry {
	seen := map[string]bool{}
	var out []Entry
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		e := Entry{Name: name, Title: name, Group: "other", Secret: true}
		if v, ok := ManifestVar(name); ok {
			e.Title, e.Hint, e.Group, e.Secret, e.Known = v.Title, v.Hint, v.Group, v.Secret, true
		} else if _, plain := cfg.Env[name]; plain {
			e.Secret = false
		}
		if e.Secret {
			if v, err := store.Get(name); err == nil && v != "" {
				e.Set = true
			}
		} else if v := strings.TrimSpace(cfg.Env[name]); v != "" {
			e.Set, e.Value = true, v
		}
		out = append(out, e)
	}
	for _, v := range Manifest {
		add(v.Name)
	}
	extra := append([]string{}, cfg.Secrets...)
	for n := range cfg.Env {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	for _, n := range extra {
		add(n)
	}
	return out
}

// SetValue сохраняет доступ: секрет — в хранилище ОС (имя — в dev.yaml),
// несекретное — в dev.yaml. plain учитывается только для имён вне манифеста.
func SetValue(home string, store *Store, name, value string, plain bool) error {
	editMu.Lock()
	defer editMu.Unlock()
	if !ValidEnvName(name) {
		return fmt.Errorf("имя %q: только A-Z, 0-9 и _, с буквы", name)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("пустое значение — чтобы убрать доступ, удалите его")
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("значение должно быть одной строкой")
	}
	if v, ok := ManifestVar(name); ok {
		plain = !v.Secret
	}
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	if plain {
		if cfg.Env == nil {
			cfg.Env = map[string]string{}
		}
		cfg.Env[name] = value
	} else {
		if err := store.Set(name, value); err != nil {
			return err
		}
		found := false
		for _, n := range cfg.Secrets {
			found = found || n == name
		}
		if !found {
			cfg.Secrets = append(cfg.Secrets, name)
		}
	}
	return cfg.Save(home)
}

// DeleteValue убирает доступ отовсюду (хранилище ОС и dev.yaml).
func DeleteValue(home string, store *Store, name string) error {
	editMu.Lock()
	defer editMu.Unlock()
	if !ValidEnvName(name) {
		return fmt.Errorf("имя %q: только A-Z, 0-9 и _, с буквы", name)
	}
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	if err := store.Delete(name); err != nil {
		return err
	}
	delete(cfg.Env, name)
	out := cfg.Secrets[:0]
	for _, n := range cfg.Secrets {
		if n != name {
			out = append(out, n)
		}
	}
	cfg.Secrets = out
	return cfg.Save(home)
}

// SetImage — образ рабочего места в dev.yaml (выбор в лаунчере).
func SetImage(home, image, flavor string) error {
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	if cfg.Image != image {
		cfg.PreviousImage = cfg.Image
	}
	cfg.Image, cfg.Flavor = image, flavor
	return cfg.Save(home)
}
