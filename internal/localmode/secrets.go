package localmode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"

	"github.com/arijjia/agctl/internal/secrets"
)

// KeyringService — имя сервиса записей в хранилище ОС.
const KeyringService = "agctl-dev"

// Var — переменная манифеста: что спросить у разработчика и как она
// называется в контейнере.
type Var struct {
	Name   string // имя env в контейнере
	Title  string // подпись в мастере
	Secret bool   // значение — в хранилище ОС; иначе — в dev.yaml
	Hint   string // подсказка (где взять)
	Group  string // llm | gitlab | jira
}

// Manifest — известные переменные локального режима. Произвольные имена тоже
// допустимы (agctl dev secrets set ИМЯ).
var Manifest = []Var{
	{Name: "AGCTL_LLM_API_KEY", Title: "Ключ модели (LLM)", Secret: true, Group: "llm",
		Hint: "выдаёт команда платформы; raw-токен без Bearer"},
	{Name: "GITLAB_URL", Title: "Адрес GitLab", Group: "gitlab", Hint: "например https://gitlab.example.org"},
	{Name: "GITLAB_TOKEN", Title: "Личный токен GitLab", Secret: true, Group: "gitlab",
		Hint: "GitLab → Preferences → Access Tokens, scope api + write_repository"},
	{Name: "JIRA_URL", Title: "Адрес Jira", Group: "jira", Hint: "например https://jira.example.org"},
	{Name: "JIRA_USER", Title: "Логин Jira", Group: "jira"},
	{Name: "JIRA_API_TOKEN", Title: "Личный токен Jira (PAT)", Secret: true, Group: "jira",
		Hint: "Jira → Профиль → Personal Access Tokens; если PAT нет — JIRA_PASSWORD"},
}

// ManifestVar — переменная манифеста по имени.
func ManifestVar(name string) (Var, bool) {
	for _, v := range Manifest {
		if v.Name == name {
			return v, true
		}
	}
	return Var{}, false
}

var envNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// ValidEnvName — имя переменной окружения ([A-Z0-9_], с буквы).
func ValidEnvName(name string) bool { return envNameRe.MatchString(name) }

// Store — секреты локального режима: хранилище ОС, а если его нет (Linux без
// Secret Service, WSL, контейнер) — зашифрованный файл в доме (AES-GCM,
// мастер-ключ 0600 рядом). Значения не логируются.
type Store struct {
	home string
	mu   sync.Mutex
	// fallback — true, если хранилище ОС недоступно и используется файл.
	fallback bool
}

// ErrSecretNotFound — секрета нет.
var ErrSecretNotFound = errors.New("секрет не задан")

// OpenStore проверяет хранилище ОС пробной записью; при недоступности —
// файловый запасной вариант. AGCTL_DEV_SECRETS=file форсирует файл.
func OpenStore(home string) *Store {
	s := &Store{home: home}
	if strings.EqualFold(os.Getenv("AGCTL_DEV_SECRETS"), "file") {
		s.fallback = true
		return s
	}
	probe := "__agctl_probe__"
	if err := keyring.Set(KeyringService, probe, "1"); err != nil {
		s.fallback = true
		return s
	}
	_ = keyring.Delete(KeyringService, probe)
	return s
}

// Backend — человекочитаемое имя хранилища.
func (s *Store) Backend() string {
	if s.fallback {
		return "зашифрованный файл " + filepath.Join(s.home, "secrets.enc")
	}
	return "хранилище ОС (" + keyringName() + ")"
}

// Set сохраняет значение.
func (s *Store) Set(name, value string) error {
	if !ValidEnvName(name) {
		return fmt.Errorf("имя %q: только A-Z, 0-9 и _, с буквы", name)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return errors.New("значение должно быть одной строкой")
	}
	if !s.fallback {
		return keyring.Set(KeyringService, name, value)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readFile()
	if err != nil {
		return err
	}
	all[name] = value
	return s.writeFile(all)
}

// Get читает значение; нет — ErrSecretNotFound.
func (s *Store) Get(name string) (string, error) {
	if !s.fallback {
		v, err := keyring.Get(KeyringService, name)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrSecretNotFound
		}
		return v, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readFile()
	if err != nil {
		return "", err
	}
	v, ok := all[name]
	if !ok {
		return "", ErrSecretNotFound
	}
	return v, nil
}

// Delete удаляет значение (отсутствие — не ошибка).
func (s *Store) Delete(name string) error {
	if !s.fallback {
		err := keyring.Delete(KeyringService, name)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readFile()
	if err != nil {
		return err
	}
	delete(all, name)
	return s.writeFile(all)
}

func (s *Store) manager() (*secrets.Manager, error) {
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		return nil, err
	}
	key, err := secrets.EnsureMasterKey(s.home)
	if err != nil {
		return nil, err
	}
	return secrets.New(key)
}

func (s *Store) readFile() (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(s.home, "secrets.enc"))
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m, err := s.manager()
	if err != nil {
		return nil, err
	}
	plain, err := m.Decrypt(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("secrets.enc не расшифровывается: %w", err)
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) writeFile(all map[string]string) error {
	m, err := s.manager()
	if err != nil {
		return err
	}
	plain, err := json.Marshal(all)
	if err != nil {
		return err
	}
	enc, err := m.Encrypt(string(plain))
	if err != nil {
		return err
	}
	path := filepath.Join(s.home, "secrets.enc")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(enc), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ContainerEnv собирает KEY=VALUE для контейнера: несекретные значения из
// dev.yaml + секреты из хранилища + производные имена для штатных
// инструментов (glab, jira-cli, MCP GitLab/Jira, тулкиты доменов). Ключ LLM
// в контейнер env не попадает — его получает сервер как провайдера.
func ContainerEnv(cfg *Config, store *Store) ([]string, []string) {
	vals := map[string]string{}
	for k, v := range cfg.Env {
		if ValidEnvName(k) && strings.TrimSpace(v) != "" {
			vals[k] = strings.TrimSpace(v)
		}
	}
	var missing []string
	for _, name := range cfg.Secrets {
		if name == "AGCTL_LLM_API_KEY" {
			continue
		}
		v, err := store.Get(name)
		if err != nil || v == "" {
			missing = append(missing, name)
			continue
		}
		vals[name] = v
	}
	alias := func(dst, src string, f func(string) string) {
		if _, ok := vals[dst]; ok {
			return
		}
		if v := vals[src]; v != "" {
			if f != nil {
				v = f(v)
			}
			vals[dst] = v
		}
	}
	trim := func(v string) string { return strings.TrimRight(v, "/") }
	host := func(v string) string {
		v = strings.TrimPrefix(strings.TrimPrefix(trim(v), "https://"), "http://")
		return v
	}
	// glab: GITLAB_HOST + GITLAB_TOKEN; MCP GitLab: GITLAB_API_URL + GITLAB_PERSONAL_ACCESS_TOKEN
	alias("GITLAB_HOST", "GITLAB_URL", host)
	alias("GITLAB_API_URL", "GITLAB_URL", func(v string) string { return trim(v) + "/api/v4" })
	alias("GITLAB_PERSONAL_ACCESS_TOKEN", "GITLAB_TOKEN", nil)
	// jira-cli: JIRA_API_TOKEN; MCP Jira (mcp-atlassian): JIRA_URL + JIRA_PERSONAL_TOKEN
	alias("JIRA_PERSONAL_TOKEN", "JIRA_API_TOKEN", nil)
	alias("JIRA_USERNAME", "JIRA_USER", nil)
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]string, 0, len(names))
	for _, k := range names {
		out = append(out, k+"="+vals[k])
	}
	return out, missing
}
