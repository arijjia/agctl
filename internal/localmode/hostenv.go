package localmode

// hostenv.go — доступы из окружения хоста. Разработчик держит токены и адреса
// в переменных окружения — одно место для всех харнессов и запусков без
// agctl. agctl dev up переносит их в дом: секреты — в хранилище, адреса и
// логины — в config.yaml. Только в пустые записи: заданное в agctl (лаунчер,
// agctl dev secrets set) не перезаписывается, а удалить значение в agctl —
// значит снова взять его из окружения на следующем up (удобно пробовать
// другие учётные записи и возвращаться к своей). Берутся только известные
// имена — манифест, ключи провайдеров, env_from профиля, — не всё окружение.

import (
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/profile"
)

// hostEnvDefaults — где искать значения в окружении: имя в agctl → имена
// переменных хоста по порядку (синонимы, принятые у инструментов).
var hostEnvDefaults = map[string][]string{
	"AGCTL_LLM_API_KEY": {"AGCTL_LLM_API_KEY"},
	"GITLAB_URL":        {"GITLAB_URL"},
	"GITLAB_TOKEN":      {"GITLAB_TOKEN", "GITLAB_PERSONAL_ACCESS_TOKEN"},
	"CONFLUENCE_URL":    {"CONFLUENCE_URL"},
	"CONFLUENCE_TOKEN":  {"CONFLUENCE_TOKEN", "CONFLUENCE_PERSONAL_TOKEN", "CONFLUENCE_API_TOKEN"},
	"JIRA_URL":          {"JIRA_URL"},
	"JIRA_USER":         {"JIRA_USER", "JIRA_USERNAME"},
	"JIRA_API_TOKEN":    {"JIRA_API_TOKEN", "JIRA_PERSONAL_TOKEN"},
}

// HostEnvImport — что перенесено из окружения (имена, без значений).
type HostEnvImport struct {
	Name   string `json:"name"`   // имя в agctl
	From   string `json:"from"`   // переменная хоста
	Secret bool   `json:"secret"` // true — в хранилище, false — в config.yaml
}

// HostEnvSources — имена в agctl и их источники в окружении хоста: встроенные
// синонимы, секреты дома и ключи личных провайдеров, extraNames (ключи
// провайдеров профиля, секреты MCP-каталога) — под своими именами; envFrom
// профиля — первыми в списке источников своего имени.
func HostEnvSources(cfg *Config, envFrom map[string][]string, extraNames ...string) map[string][]string {
	out := map[string][]string{}
	add := func(name string, srcs ...string) {
		if !ValidEnvName(name) {
			return
		}
		for _, s := range srcs {
			if !ValidEnvName(s) {
				continue
			}
			dup := false
			for _, have := range out[name] {
				dup = dup || have == s
			}
			if !dup {
				out[name] = append(out[name], s)
			}
		}
	}
	for name, srcs := range envFrom {
		add(name, srcs...)
	}
	for name, srcs := range hostEnvDefaults {
		add(name, srcs...)
	}
	for _, n := range cfg.Secrets {
		add(n, n)
	}
	for _, p := range cfg.Providers {
		n := ProviderKeyEnv(p.Name)
		add(n, n)
	}
	for _, n := range extraNames {
		add(n, n)
	}
	return out
}

// hostEnvSecret — значение имени хранится как секрет: по манифесту, ключи
// моделей и имена вида …_KEY, …_TOKEN, пароли.
func hostEnvSecret(name string) bool {
	if v, ok := ManifestVar(name); ok {
		return v.Secret
	}
	return IsModelKeyEnv(name) || profile.SecretName(name)
}

// ImportHostEnv переносит в дом значения из окружения хоста (getenv) для
// пустых записей по sources (HostEnvSources). config.yaml env_import: false —
// ничего не делает. Возвращает перенесённое (имена, без значений).
func ImportHostEnv(home string, store *Store, sources map[string][]string, getenv func(string) string) ([]HostEnvImport, error) {
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return nil, err
	}
	if !cfg.EnvImportOn() {
		return nil, nil
	}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []HostEnvImport
	changed := false
	for _, name := range names {
		val, from := "", ""
		for _, src := range sources[name] {
			if v := strings.TrimSpace(getenv(src)); v != "" && !strings.ContainsAny(v, "\r\n\x00") {
				val, from = v, src
				break
			}
		}
		if val == "" {
			continue
		}
		secret := hostEnvSecret(name)
		if secret {
			if v, err := store.Get(name); err == nil && v != "" {
				continue
			}
			if err := store.Set(name, val); err != nil {
				return out, err
			}
			found := false
			for _, n := range cfg.Secrets {
				found = found || n == name
			}
			if !found {
				cfg.Secrets = append(cfg.Secrets, name)
				changed = true
			}
		} else {
			if strings.TrimSpace(cfg.Env[name]) != "" {
				continue
			}
			if cfg.Env == nil {
				cfg.Env = map[string]string{}
			}
			cfg.Env[name] = val
			changed = true
		}
		out = append(out, HostEnvImport{Name: name, From: from, Secret: secret})
	}
	if changed {
		if err := cfg.Save(home); err != nil {
			return out, err
		}
	}
	return out, nil
}
