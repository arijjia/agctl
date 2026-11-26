package localmode

// providers.go — личные провайдеры моделей разработчика. Организация задаёт
// своих провайдеров в профиле (profile.yaml: llm), разработчик добавляет
// своих поверх — в настройках дома (providers:). Ключ провайдера лежит в
// хранилище секретов под именем ProviderKeyEnv(name); в файле настроек —
// только имя провайдера, адрес и модели.

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Provider — личный провайдер моделей (OpenAI-совместимый API).
type Provider struct {
	// Name — имя провайдера: под ним он виден в лаунчере и в списке моделей
	// харнесса. Строчная латиница, цифры, дефис, точка.
	Name string `yaml:"name" json:"name"`
	// BaseURL — адрес API (https://…/v1).
	BaseURL string `yaml:"base_url" json:"baseURL"`
	// Models — модели провайдера в списке харнесса.
	Models []string `yaml:"models" json:"models"`
	// DefaultModel — модель по умолчанию; пусто — первая из Models.
	DefaultModel string `yaml:"default_model,omitempty" json:"defaultModel,omitempty"`
}

const (
	providerKeyPrefix = "AGCTL_PROVIDER_"
	providerKeySuffix = "_API_KEY"
)

// ProviderKeyEnv — имя секрета с ключом личного провайдера.
func ProviderKeyEnv(name string) string {
	up := strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(strings.TrimSpace(name)))
	return providerKeyPrefix + up + providerKeySuffix
}

// IsModelKeyEnv — имя секрета с ключом модели: такие значения получает
// сервер как ключ провайдера, в окружение контейнера они не попадают.
// Соглашение об именах: AGCTL_LLM_* (ключи провайдеров профиля) и
// AGCTL_PROVIDER_* (ключи личных провайдеров).
func IsModelKeyEnv(name string) bool {
	return strings.HasPrefix(name, "AGCTL_LLM_") || strings.HasPrefix(name, providerKeyPrefix)
}

// NormalizeProvider приводит и проверяет описание провайдера.
func NormalizeProvider(p Provider) (Provider, error) {
	p.Name = strings.ToLower(strings.TrimSpace(p.Name))
	if p.Name == "" || len(p.Name) > 40 {
		return p, errors.New("имя провайдера: от 1 до 40 символов")
	}
	for i, r := range p.Name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || (i > 0 && (r == '-' || r == '.'))
		if !ok {
			return p, errors.New("имя провайдера: строчные латинские буквы, цифры, дефис и точка; начинается с буквы или цифры")
		}
	}
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	u, err := url.Parse(p.BaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return p, errors.New("адрес API: нужен полный адрес вида https://host/v1")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, m := range p.Models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		if strings.ContainsAny(m, " \t\r\n") {
			return p, fmt.Errorf("модель %q: имя без пробелов", m)
		}
		seen[m] = true
		models = append(models, m)
	}
	if len(models) == 0 {
		return p, errors.New("укажите хотя бы одну модель")
	}
	p.Models = models
	p.DefaultModel = strings.TrimSpace(p.DefaultModel)
	if p.DefaultModel != "" && !seen[p.DefaultModel] {
		return p, fmt.Errorf("модель по умолчанию %q не входит в список моделей", p.DefaultModel)
	}
	return p, nil
}

// SaveProvider добавляет или обновляет личного провайдера в настройках дома.
// key: nil — ключ не трогать; иначе новое значение (в хранилище секретов).
func SaveProvider(home string, store *Store, p Provider, key *string) error {
	p, err := NormalizeProvider(p)
	if err != nil {
		return err
	}
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	replaced := false
	for i := range cfg.Providers {
		if cfg.Providers[i].Name == p.Name {
			cfg.Providers[i], replaced = p, true
		}
	}
	if !replaced {
		cfg.Providers = append(cfg.Providers, p)
	}
	env := ProviderKeyEnv(p.Name)
	if key != nil {
		k := strings.TrimSpace(*key)
		if strings.ContainsAny(k, "\r\n\x00") {
			return errors.New("ключ должен быть одной строкой")
		}
		if k == "" {
			_ = store.Delete(env)
			cfg.Secrets = dropName(cfg.Secrets, env)
		} else {
			if err := store.Set(env, k); err != nil {
				return err
			}
			cfg.Secrets = addName(cfg.Secrets, env)
		}
	}
	return cfg.Save(home)
}

// DeleteProvider убирает личного провайдера и его ключ.
func DeleteProvider(home string, store *Store, name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	kept := cfg.Providers[:0]
	found := false
	for _, p := range cfg.Providers {
		if p.Name == name {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("провайдер %q не найден", name)
	}
	cfg.Providers = kept
	env := ProviderKeyEnv(name)
	_ = store.Delete(env)
	cfg.Secrets = dropName(cfg.Secrets, env)
	return cfg.Save(home)
}

func addName(list []string, name string) []string {
	for _, n := range list {
		if n == name {
			return list
		}
	}
	return append(list, name)
}

func dropName(list []string, name string) []string {
	out := list[:0]
	for _, n := range list {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}
