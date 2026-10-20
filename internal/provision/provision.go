// Package provision — создание «репки + рабочего клона + dsh-home»:
// git в двух местах (bare-origin «эмуляция GitLab» + смонтированный клон),
// шаблон репы со скиллами и стартовой карточкой бэклога, скелет DSH_HOME
// с конфигом провайдера и токеном из личного кабинета. Autosave —
// периодический commit+push как страховка поверх коммитов агента.
package provision

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	BaseURL string   // например https://litellm-dev.utmn.ru/v1
	Model   string   // модель по умолчанию (первая из Models)
	Models  []string // все доступные модели (выпадающий список в dsh)
	APIKey  string   // токен — только в .credentials.yaml и env контейнера
}

// DefaultModel — модель по умолчанию: Model, иначе первая из Models.
func (p Provider) DefaultModel() string {
	if p.Model != "" {
		return p.Model
	}
	if len(p.Models) > 0 {
		return p.Models[0]
	}
	return ""
}

// AllModels — полный список моделей (Models, иначе одна Model).
func (p Provider) AllModels() []string {
	if len(p.Models) > 0 {
		return p.Models
	}
	if p.Model != "" {
		return []string{p.Model}
	}
	return nil
}

// ParseModels — разбор списка моделей из формы (по одной на строку,
// запятые тоже разрешены). Пустые строки и дубликаты отбрасываются.
func ParseModels(text, fallback string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
	}
	for _, line := range strings.Split(text, "\n") {
		for _, m := range strings.Split(line, ",") {
			add(m)
		}
	}
	if len(out) == 0 && fallback != "" {
		add(fallback)
	}
	return out
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

// ValidTemplate — legacy-совместимая проверка ID стартового комплекта.
func ValidTemplate(id string) bool {
	if id == "" {
		return true // пусто = base
	}
	_, ok := FindKit(id)
	return ok
}

// Provision сохраняет старый API и разворачивает default-компоненты комплекта.
func Provision(l Layout, p Provider, templateName string, templateFS, skelFS fs.FS) error {
	kitID, components, err := NormalizeComponents(templateName, nil)
	if err != nil {
		return err
	}
	return ProvisionWithComponents(l, p, kitID, components, templateFS, skelFS)
}

// Layout — пути воркспейса на диске хоста.
type Layout struct {
	Repo              string // $HOME/repos/<owner>/<name>.git — bare origin
	Work              string // $HOME/work/<id>/repo — рабочий клон (монтируется)
	DshHome           string // $HOME/work/<id>/dsh-home — DSH_HOME (переживает рестарт)
	ChatSessionScoped bool   // режим Chat: выделять скрытый cwd на каждую DSH-сессию
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

// ProvisionWithComponents создаёт git-пару и DSH_HOME из выбранного состава.
func ProvisionWithComponents(l Layout, p Provider, kitID string, components []string, templateFS, skelFS fs.FS) error {
	if p.BaseURL == "" || p.DefaultModel() == "" || p.APIKey == "" {
		return errors.New("provision: не заданы провайдер или токен")
	}
	if len(p.AllModels()) == 0 {
		p.Models = []string{p.Model}
	}
	if !ValidTemplate(kitID) {
		return fmt.Errorf("provision: неизвестный комплект %q", kitID)
	}
	_, normalizedComponents, err := NormalizeComponents(kitID, components)
	if err != nil {
		return fmt.Errorf("provision: %w", err)
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
	if err := CopySelectedComponents(templateFS, kitID, normalizedComponents, l.Work); err != nil {
		return fmt.Errorf("provision: компоненты комплекта: %w", err)
	}
	if err := WriteKitManifest(l.Work, kitID, normalizedComponents); err != nil {
		return fmt.Errorf("provision: манифест комплекта: %w", err)
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
	if err := seedWorkspaceRegistry(home); err != nil {
		return err
	}
	return RefreshProvider(home, p)
}

// EnsureChatPlugins synchronizes the Host/client plugins used only by Chat
// homes. It is idempotent and also upgrades DSH_HOME volumes seeded by older
// agctl builds. Dev workspaces keep the ordinary DSH web UI.
func EnsureChatPlugins(home string, skelFS fs.FS) error {
	if skelFS == nil {
		return fmt.Errorf("provision: chat plugin skeleton is nil")
	}
	bundles := []string{chatSessionFilesBundle, chatBrandBundle}
	for _, bundle := range bundles {
		dst := filepath.Join(home, "profiles", "web", "node_modules", filepath.FromSlash(bundle))
		src := "dsh-skel/profiles/web/node_modules/" + bundle
		if err := CopyFS(skelFS, src, dst); err != nil {
			return fmt.Errorf("provision: sync %s: %w", bundle, err)
		}
	}

	pjPath := filepath.Join(home, "profiles", "web", "package.json")
	raw, err := os.ReadFile(pjPath)
	if err != nil {
		return fmt.Errorf("provision: chat profile package.json: %w", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("provision: chat profile manifest: %w", err)
	}
	dsh, ok := manifest["dsh"].(map[string]any)
	if !ok {
		dsh = map[string]any{}
		manifest["dsh"] = dsh
	}
	profile, ok := dsh["profile"].(map[string]any)
	if !ok {
		profile = map[string]any{}
		dsh["profile"] = profile
	}
	bundlesAny, _ := profile["bundles"].([]any)
	changed := false
	for _, bundle := range bundles {
		has := false
		for _, b := range bundlesAny {
			if s, _ := b.(string); s == bundle {
				has = true
				break
			}
		}
		if !has {
			bundlesAny = append(bundlesAny, bundle)
			changed = true
		}
	}
	if changed {
		profile["bundles"] = bundlesAny
		out, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(pjPath, append(out, '\n'), 0o644); err != nil {
			return fmt.Errorf("provision: chat profile write: %w", err)
		}
	}

	patchPath := filepath.Join(home, "profiles", "web", "cordis.patch.yml")
	patch, err := os.ReadFile(patchPath)
	if os.IsNotExist(err) {
		patch, err = fs.ReadFile(skelFS, "dsh-skel/profiles/web/cordis.patch.yml")
	}
	if err != nil {
		return fmt.Errorf("provision: chat profile patch: %w", err)
	}
	entries := []struct{ id, bundle string }{
		{"agctl-chat-session-files", chatSessionFilesBundle},
		{"agctl-chat-brand", chatBrandBundle},
	}
	for _, item := range entries {
		if bytes.Contains(patch, []byte(item.bundle)) {
			continue
		}
		patch = append(patch, []byte(fmt.Sprintf("- id: %s\n  name: %s\n", item.id, yamlStr(item.bundle)))...)
	}
	if err := os.WriteFile(patchPath, patch, 0o644); err != nil {
		return fmt.Errorf("provision: chat profile patch write: %w", err)
	}
	return nil
}

const (
	chatSessionFilesBundle = "@arijjia/agctl-chat-session-files"
	chatBrandBundle        = "@arijjia/agctl-chat-brand"
)

// seedWorkspaceRegistry — предзаписанный реестр воркспейсов dsh: одна запись
// на /workspace. Без неё dsh при первом буте пытается создать «дефолтный»
// воркспейс в ~/Documents (xdg-user-dir отсутствует в контейнере) и падает
// с тостом «Unable to create default workspace», а сайдбар файлов остаётся
// пустым. Формат — workspace domain v2 (dsh-workspace/lib/types/spec.js).
func seedWorkspaceRegistry(home string) error {
	dir := filepath.Join(home, "storages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("provision: storages: %w", err)
	}
	path := filepath.Join(dir, "workspace.json")
	if _, err := os.Stat(path); err == nil {
		return nil // уже есть — не трогаем (переживает рестарты)
	}
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	data := fmt.Sprintf(`{
  "unit": {"name": "workspace", "version": 2},
  "global": {
    "initialized": true,
    "defaultWorkspaceId": "ws-agctl",
    "workspaceIds": ["ws-agctl"],
    "archivedSessionIds": [],
    "pinnedSessionIds": []
  },
  "tables": {
    "workspaces": {
      "ws-agctl": {
        "path": "/workspace",
        "title": "default-workspace",
        "sessionIds": [],
        "createdAt": %s,
        "updatedAt": %s
      }
    }
  }
}`, jsonStr(now), jsonStr(now))
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("provision: workspace.json: %w", err)
	}
	return nil
}

// jsonStr — строка в JSON (для workspace.json).
func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// RefreshProvider — перезапись настроек провайдера в существующем DSH_HOME
// (ротация ключа, смена Base URL/моделей). Сессии и профили не трогает:
// dsh импортирует свежий settings.yaml при следующем старте.
func RefreshProvider(home string, p Provider) error {
	models := p.AllModels()
	var sb strings.Builder
	sb.WriteString("# Настройки воркспейса agctl: провайдер из личного кабинета.\nllm-pi-ai:\n  providers:\n    agctl:\n      apiKeyEnv: AGCTL_LLM_API_KEY\n      api: openai-completions\n")
	sb.WriteString("      baseURL: " + yamlStr(p.BaseURL) + "\n      models:\n")
	for _, m := range models {
		sb.WriteString("        - id: " + yamlStr(m) + "\n          name: " + yamlStr(m) +
			"\n          contextWindow: 262144\n          maxTokens: 65536\n")
	}
	sb.WriteString("agent-default-model:\n  provider: agctl\n  model: " + yamlStr(p.DefaultModel()) + "\n")
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("provision: settings.yaml: %w", err)
	}
	// Пустой токен — не пишем ключ вовсе: dsh-credentials-local падает на
	// пустом значении («remove the key instead») и показывает свой модал
	// ввода API-ключа (паттерн общего чата Tier 0).
	creds := "version: 1\nrefs: {}\n"
	if p.APIKey != "" {
		creds = fmt.Sprintf(`version: 1
refs:
  AGCTL_LLM_API_KEY: %s
`, yamlStr(p.APIKey))
	}
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
