// Package provision — создание «репки + рабочего клона + dsh-home»:
// git в двух местах (bare-origin «эмуляция GitLab» + смонтированный клон),
// шаблон репы со скиллами и стартовой карточкой бэклога, скелет DSH_HOME
// с конфигом провайдера и токеном из личного кабинета. Autosave —
// периодический commit+push как страховка поверх коммитов агента.
package provision

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// Provider — настройки LLM-провайдера воркспейса (из личного кабинета).
type Provider struct {
	BaseURL string // например https://litellm-dev.utmn.ru/v1
	Model   string // например qwen-3.8-deep
	APIKey  string // токен — только в .credentials.yaml и env контейнера
}

// Template — зарегистрированный шаблон репки.
type Template struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Templates — реестр шаблонов (ID = подкаталог в assets templates/<id>).
var Templates = []Template{
	{
		ID:    "base",
		Title: "Базовое рабочее место",
		Description: "Агент с правилом частых коммитов, скиллом ведения бэклогов " +
			"и саморедактируемыми процедурами. Универсальный старт.",
	},
	{
		ID:    "1c-vibe",
		Title: "1С-разработка (1c-vibe)",
		Description: "Цикл Jira → git-ветка → правка выгрузки 1С (XML/BSL) → " +
			"самопроверка → MR через publish.py. Тулзы контура и демо-проект включены.",
	},
}

// ValidTemplate — есть ли такой шаблон.
func ValidTemplate(id string) bool {
	if id == "" {
		return true // пусто = base
	}
	for _, t := range Templates {
		if t.ID == id {
			return true
		}
	}
	return false
}

func templatePrefix(name string) string {
	if name == "" {
		name = "base"
	}
	return "templates/" + name
}

// Layout — пути воркспейса на диске хоста.
type Layout struct {
	Repo    string // $HOME/repos/<owner>/<name>.git — bare origin
	Work    string // $HOME/work/<id>/repo — рабочий клон (монтируется)
	DshHome string // $HOME/work/<id>/dsh-home — DSH_HOME (переживает рестарт)
}

// NewID — короткий id воркспейса: w-<12 hex>.
func NewID() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "w-" + hex.EncodeToString(b), nil
}

// Paths — раскладка путей для нового воркспейса.
func Paths(home, ownerLogin, wsID, name string) Layout {
	safe := sanitize(name)
	return Layout{
		Repo:    filepath.Join(home, "repos", sanitize(ownerLogin), safe+".git"),
		Work:    filepath.Join(home, "work", wsID, "repo"),
		DshHome: filepath.Join(home, "work", wsID, "dsh-home"),
	}
}

func sanitize(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "workspace"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// Provision создаёт всю файловую часть воркспейса:
// bare-репу из шаблона templateName + рабочий клон + скелет DSH_HOME с токеном.
// templateFS и skelFS — встроенные ассеты (internal/assets).
func Provision(l Layout, p Provider, templateName string, templateFS, skelFS fs.FS) error {
	if p.BaseURL == "" || p.Model == "" || p.APIKey == "" {
		return errors.New("provision: не заданы провайдер или токен")
	}
	if !ValidTemplate(templateName) {
		return fmt.Errorf("provision: неизвестный шаблон %q", templateName)
	}
	for _, d := range []string{l.Repo, filepath.Dir(l.Work), l.DshHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("provision: mkdir: %w", err)
		}
	}

	// 1. bare-origin («эмуляция GitLab»)
	bare, err := git.PlainInitWithOptions(l.Repo, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		Bare:        true,
	})
	if err != nil {
		return fmt.Errorf("provision: bare init: %w", err)
	}

	// 2. рабочий клон: init + шаблон + первый коммит + push в origin
	repo, err := git.PlainInitWithOptions(l.Work, &git.PlainInitOptions{
		InitOptions: git.InitOptions{DefaultBranch: plumbing.NewBranchReferenceName("main")},
		Bare:        false,
	})
	if err != nil {
		return fmt.Errorf("provision: work init: %w", err)
	}
	if err := CopyFS(templateFS, templatePrefix(templateName), l.Work); err != nil {
		return fmt.Errorf("provision: шаблон репы: %w", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return fmt.Errorf("provision: git add: %w", err)
	}
	// идентичность коммитов агента (иначе git внутри контейнера откажет)
	if cfg, err := repo.Config(); err == nil {
		cfg.User.Name = "agent"
		cfg.User.Email = "agent@workspace.local"
		if err := repo.Storer.SetConfig(cfg); err != nil {
			return fmt.Errorf("provision: git config: %w", err)
		}
	}
	sig := signature()
	if _, err := wt.Commit("init: шаблон рабочего места (AGENTS.md, skills, backlog)",
		&git.CommitOptions{Author: sig, Committer: sig, AllowEmptyCommits: false}); err != nil {
		return fmt.Errorf("provision: первый коммит: %w", err)
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin", URLs: []string{l.Repo},
	}); err != nil {
		return fmt.Errorf("provision: remote origin: %w", err)
	}
	if err := repo.Push(&git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
	}); err != nil {
		return fmt.Errorf("provision: push в bare: %w", err)
	}
	// HEAD bare → main, чтобы clone из origin сразу попадал на main.
	if err := bare.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		return fmt.Errorf("provision: bare HEAD: %w", err)
	}

	// 3. DSH_HOME: скелет + настройки провайдера + токен (chmod 600)
	if err := SeedDshHome(l.DshHome, p, skelFS); err != nil {
		return err
	}
	return nil
}

func signature() *object.Signature {
	return &object.Signature{Name: "agctl", Email: "agctl@localhost", When: time.Now()}
}

// CopyFS копирует поддерево fs.FS в dst (пропуская корневой префикс).
func CopyFS(fsys fs.FS, prefix, dst string) error {
	return fs.WalkDir(fsys, prefix, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(prefix, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// SeedDshHome — скелет DSH_HOME (проверенный паттерн kb/dsh/mgmt) + provider.
func SeedDshHome(home string, p Provider, skelFS fs.FS) error {
	if err := CopyFS(skelFS, "dsh-skel", home); err != nil {
		return fmt.Errorf("provision: скелет dsh-home: %w", err)
	}
	settings := fmt.Sprintf(`# Настройки воркспейса agctl: провайдер из личного кабинета.
llm-pi-ai:
  providers:
    agctl:
      apiKeyEnv: AGCTL_LLM_API_KEY
      api: openai-completions
      baseURL: %s
      models:
        - id: %s
          name: %s
          contextWindow: 262144
          maxTokens: 65536
agent-default-model:
  provider: agctl
  model: %s
`, yamlStr(p.BaseURL), yamlStr(p.Model), yamlStr(p.Model), yamlStr(p.Model))
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(settings), 0o644); err != nil {
		return fmt.Errorf("provision: settings.yaml: %w", err)
	}
	creds := fmt.Sprintf(`version: 1
refs:
  AGCTL_LLM_API_KEY: %s
`, yamlStr(p.APIKey))
	if err := os.WriteFile(filepath.Join(home, ".credentials.yaml"), []byte(creds), 0o600); err != nil {
		return fmt.Errorf("provision: .credentials.yaml: %w", err)
	}
	return nil
}

// yamlStr — безопасная запись строки в yaml (в кавычках).
func yamlStr(s string) string {
	q := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(s)
	return "\"" + q + "\""
}

// AutosaveOnce — страховочный commit+push рабочего клона в bare-origin.
// Возвращает true, если был новый autosave-коммит. Чистое дерево — no-op.
func AutosaveOnce(workPath string) (bool, error) {
	r, err := git.PlainOpen(workPath)
	if err != nil {
		return false, fmt.Errorf("autosave: open: %w", err)
	}
	wt, err := r.Worktree()
	if err != nil {
		return false, err
	}
	st, err := wt.Status()
	if err != nil {
		return false, fmt.Errorf("autosave: status: %w", err)
	}
	push := func() error {
		err := r.Push(&git.PushOptions{
			RemoteName: "origin",
			RefSpecs:   []config.RefSpec{"refs/heads/main:refs/heads/main"},
		})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return fmt.Errorf("autosave: push: %w", err)
		}
		return nil
	}
	if st.IsClean() {
		// коммиты могли сделать агент/человек — доставляем до origin
		return false, push()
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		return false, fmt.Errorf("autosave: add: %w", err)
	}
	sig := signature()
	if _, err := wt.Commit(fmt.Sprintf("autosave: %s", time.Now().UTC().Format("2006-01-02 15:04 MST")),
		&git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		return false, fmt.Errorf("autosave: commit: %w", err)
	}
	return true, push()
}
