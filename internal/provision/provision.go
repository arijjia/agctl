// Package provision — создание «репки + рабочего клона + dsh-home»:
// git в двух местах (bare-origin «эмуляция GitLab» + смонтированный клон),
// шаблон репы со скиллами и стартовой карточкой бэклога, скелет DSH_HOME
// с конфигом провайдера и токеном из личного кабинета. Autosave —
// периодический commit+push как страховка поверх коммитов агента.
package provision

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"gopkg.in/yaml.v3"
)

// Provider — настройки LLM-провайдера воркспейса (из личного кабинета).
type Provider struct {
	// Name — алиас провайдера в dsh (llm-pi-ai.providers.<Name>, имя в
	// выпадающем списке моделей); пусто = хост BaseURL.
	Name     string
	BaseURL  string   // primary/legacy base URL (первый элемент BaseURLs)
	BaseURLs []string // несколько provider endpoints с общим списком Models
	Model    string   // модель по умолчанию (первая из Models)
	Models   []string // все доступные модели (выпадающий список в dsh)
	APIKey   string   // токен — только в .credentials.yaml и env контейнера
	// Extras — дополнительные провайдеры со своими эндпоинтами, моделями и
	// ключами; попадают в тот же выпадающий список dsh.
	Extras []ExtraProvider
}

// ExtraProvider — второй (третий, …) провайдер в выпадающем списке dsh:
// другой эндпоинт, другой список моделей и СВОЙ ключ. Ключ приходит из env
// хоста по имени APIKeyEnv (в profile.yaml хранится только имя переменной,
// не секрет). Пример: корпоративный LiteLLM рядом с подпиской Z.AI.
type ExtraProvider struct {
	Name      string   // алиас в dsh (llm-pi-ai.providers.<Name>)
	BaseURL   string   // OpenAI-совместимый эндпоинт
	Model     string   // дефолтная модель алиаса (пусто = первая из Models)
	Models    []string // модели алиаса в выпадашке
	APIKeyEnv string   // имя env-переменной с ключом (реф в .credentials.yaml)
	APIKey    string   // значение из env; пусто = алиас без ключа (dsh спросит сам)
}

// AllBaseURLs — список URL в порядке выбора, старые вызовы BaseURL-only
// остаются совместимы. BaseURL всегда ставится первым и дубликаты убираются.
func (p Provider) AllBaseURLs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		u := strings.TrimSpace(strings.TrimRight(raw, "/"))
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	add(p.BaseURL)
	for _, u := range p.BaseURLs {
		add(u)
	}
	return out
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

// Alias — имя основного провайдера в dsh. Имя не задано — берётся хост его
// API (api.z.ai): провайдер виден под своим именем, а не под именем
// платформы.
func (p Provider) Alias() string { return providerAlias(p.Name, hostAlias(p.BaseURL, "provider")) }

// hostAlias — имя провайдера из хоста адреса API; адреса нет — fallback.
func hostAlias(baseURL, fallback string) string {
	raw := strings.TrimSpace(baseURL)
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/:?#"); i >= 0 {
		raw = raw[:i]
	}
	return providerAlias(raw, fallback)
}

// AllProviderURLs — все эндпоинты всех провайдеров: основной (со всеми
// BaseURLs-алиасами) и дополнительные. Нужен сетевому слою для DNS-пинов.
func (p Provider) AllProviderURLs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		u := strings.TrimRight(strings.TrimSpace(raw), "/")
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, u := range p.AllBaseURLs() {
		add(u)
	}
	for _, extra := range p.Extras {
		add(extra.BaseURL)
	}
	return out
}

// CredentialEnv — пары env-переменная → ключ для .credentials.yaml и env
// контейнера. Пустые ключи не попадают: dsh-credentials-local падает на
// пустом значении и показывает свой модал ввода.
func (p Provider) CredentialEnv() map[string]string {
	out := map[string]string{}
	if p.APIKey != "" {
		out["AGCTL_LLM_API_KEY"] = p.APIKey
	}
	for _, extra := range p.Extras {
		if extra.APIKey == "" {
			continue
		}
		out[extra.KeyEnv()] = extra.APIKey
	}
	return out
}

// providerAlias нормализует имя провайдера под ключ YAML/выпадашки dsh.
func providerAlias(raw, fallback string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}

// AllModels — список моделей дополнительного провайдера.
func (e ExtraProvider) AllModels() []string {
	if len(e.Models) > 0 {
		return e.Models
	}
	if e.Model != "" {
		return []string{e.Model}
	}
	return nil
}

// DefaultModel — модель по умолчанию дополнительного провайдера.
func (e ExtraProvider) DefaultModel() string {
	if e.Model != "" {
		return e.Model
	}
	if len(e.Models) > 0 {
		return e.Models[0]
	}
	return ""
}

// Alias — безопасный алиас провайдера в dsh; имя не задано — хост API.
func (e ExtraProvider) Alias() string { return providerAlias(e.Name, hostAlias(e.BaseURL, "provider")) }

// KeyEnv — имя env-переменной с ключом (реф в .credentials.yaml).
func (e ExtraProvider) KeyEnv() string {
	if env := strings.TrimSpace(e.APIKeyEnv); env != "" {
		return env
	}
	alias := strings.NewReplacer("-", "_", ".", "_").Replace(e.Alias())
	return "AGCTL_LLM_API_KEY_" + strings.ToUpper(alias)
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

// Layout — пути рантайма на диске хоста (v3: только чат-контейнер).
type Layout struct {
	Repo    string // $HOME/repos/<owner>/<name>.git — bare origin (v2-наследие, v3 не использует)
	Work    string // рабочий клон/каталог (монтируется в /workspace)
	DshHome string // DSH_HOME (переживает рестарт)
}

// signature — авторство автоматических коммитов (autosave, seed).
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

// EnsurePermissionDefaults — роль user: закрепить пресет разрешений
// danger-full-access дефолтом свежих сессий в профиле DSH (permission-presets).
// Для developer/admin роль-запись удаляется: пресет-селектор остаётся штатным.
// Идемпотентно: повторный ensure обновляет запись, снятие флага её удаляет.
func EnsurePermissionDefaults(home string, fullAccess bool) error {
	profileDir := filepath.Join(home, "profiles", "web")
	patchPath := filepath.Join(profileDir, "cordis.patch.yml")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return fmt.Errorf("provision: permission defaults dir: %w", err)
	}
	type patchEntry = map[string]any
	var entries []patchEntry
	if raw, err := os.ReadFile(patchPath); err == nil {
		if len(bytes.TrimSpace(raw)) > 0 {
			if err := yaml.Unmarshal(raw, &entries); err != nil {
				return fmt.Errorf("provision: permission defaults parse %s: %w", patchPath, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("provision: permission defaults read %s: %w", patchPath, err)
	}
	const presetPlugin = "@deepseek-ai/dsh-permission-presets"
	kept := entries[:0]
	found := false
	for _, entry := range entries {
		if name, _ := entry["name"].(string); name == presetPlugin {
			found = true
			continue // перевставляем ниже с актуальным конфигом
		}
		kept = append(kept, entry)
	}
	if !fullAccess && !found {
		return nil // developer/admin без прежней user-настройки: файл не трогаем
	}
	if fullAccess {
		kept = append(kept, patchEntry{
			"id":   "permission",
			"name": presetPlugin,
			"config": map[string]any{
				"defaultPreset": "danger-full-access",
			},
		})
	}
	if len(kept) == 0 && !found {
		return nil // нечего и незачем создавать
	}
	out, err := yaml.Marshal(kept)
	if err != nil {
		return fmt.Errorf("provision: permission defaults marshal: %w", err)
	}
	header := []byte("# agctl: defaults пресетов разрешений синхронизируются с ролью пользователя.\n")
	if err := os.WriteFile(patchPath, append(header, out...), 0o644); err != nil {
		return fmt.Errorf("provision: permission defaults write %s: %w", patchPath, err)
	}
	return nil
}

// EnsureChatPlugins synchronizes the Host/client plugins used only by Chat
// homes. It is idempotent and also upgrades DSH_HOME volumes seeded by older
// agctl builds. Dev workspaces keep the ordinary DSH web UI.
func EnsureChatPlugins(home string, skelFS fs.FS) error {
	return ensureChatPlugins(home, skelFS, false)
}

// EnsureChatPluginsLocal — то же для локального режима (agctl dev): плагин
// «Кабинет» (профиль, роли, статистика, SSH, справка платформы) локально не
// нужен — его нет в бандлах, а на томе, засеянном раньше, он вычищается из
// node_modules, package.json и cordis.patch.yml.
func EnsureChatPluginsLocal(home string, skelFS fs.FS) error {
	return ensureChatPlugins(home, skelFS, true)
}

func ensureChatPlugins(home string, skelFS fs.FS, local bool) error {
	if skelFS == nil {
		return fmt.Errorf("provision: chat plugin skeleton is nil")
	}
	bundles := []string{cabinetBundle, chatSessionFilesBundle, chatBrandBundle, workspaceConsoleBundle}
	if local {
		bundles = []string{chatSessionFilesBundle, chatBrandBundle, workspaceConsoleBundle}
		if err := os.RemoveAll(filepath.Join(home, "profiles", "web", "node_modules", filepath.FromSlash(cabinetBundle))); err != nil {
			return fmt.Errorf("provision: remove cabinet: %w", err)
		}
	}
	for _, bundle := range bundles {
		dst := filepath.Join(home, "profiles", "web", "node_modules", filepath.FromSlash(bundle))
		src := "dsh-skel/profiles/web/node_modules/" + bundle
		if err := CopyFS(skelFS, src, dst); err != nil {
			return fmt.Errorf("provision: sync %s: %w", bundle, err)
		}
		// Compat entry: загрузчик профилей dsh резолвит клиентский бандл как
		// <bundle>/client.js (экспорт-карту lib/client.js он не читает). Без
		// этого файла на старых томах грузится устаревшая корневая копия.
		// Host-плагины без клиентской половины (session-files) пропускаем.
		clientSrc, clientErr := fs.ReadFile(skelFS, src+"/lib/client.js")
		switch {
		case clientErr == nil:
			if err := os.WriteFile(filepath.Join(dst, "client.js"), clientSrc, 0o644); err != nil {
				return fmt.Errorf("provision: sync %s: client entry: %w", bundle, err)
			}
		case os.IsNotExist(clientErr):
			// host-only бандл: клиентского entry нет и не было.
		default:
			return fmt.Errorf("provision: sync %s: client bundle: %w", bundle, clientErr)
		}
		// Зеркальная очистка: файлы, которых больше нет в скелете, удаляем —
		// иначе на старых томах живут забытые версии (например, корневой
		// client.js древней «плоской» сборки). Сгенерированный entry — наш.
		if err := pruneStaleBundleFiles(dst, src, skelFS); err != nil {
			return fmt.Errorf("provision: sync %s: prune: %w", bundle, err)
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
	if local {
		kept := bundlesAny[:0:0]
		for _, b := range bundlesAny {
			if s, _ := b.(string); s == cabinetBundle {
				changed = true
				continue
			}
			kept = append(kept, b)
		}
		bundlesAny = kept
	}
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
		{"agctl-cabinet", cabinetBundle},
		{"agctl-chat-session-files", chatSessionFilesBundle},
		{"agctl-chat-brand", chatBrandBundle},
		{"agctl-workspace-console", workspaceConsoleBundle},
	}
	if local {
		entries = entries[1:]
		if patch, err = dropPatchPlugin(patch, cabinetBundle); err != nil {
			return err
		}
	}
	for _, item := range entries {
		if bytes.Contains(patch, []byte(item.bundle)) {
			continue
		}
		patch = append(patch, []byte(fmt.Sprintf("- id: %s\n  name: %s\n", item.id, yamlStr(item.bundle)))...)
	}
	// The product does not use a DeepSeek cloud account. Disable the native
	// account launcher so More/Feedback/Sign in and Get started do not replace
	// the ordinary DSH Settings trigger; the profile's platform API key/models
	// are supplied by agctl instead.
	const (
		accountBundle        = "@deepseek-ai/dsh-client-ui-settings-account"
		accountLauncherPatch = "- id: ui-settings-account\n  name: \"@deepseek-ai/dsh-client-ui-settings-account\"\n  disabled: true\n"
	)
	// Ищем по имени бандла, а не по точному тексту блока: после YAML-нормализации
	// (syncLLMInPatch) порядок ключей другой, и точное совпадение давало дубль.
	if !bytes.Contains(patch, []byte(accountBundle)) {
		if len(patch) > 0 && patch[len(patch)-1] != '\n' {
			patch = append(patch, '\n')
		}
		patch = append(patch, []byte(accountLauncherPatch)...)
	}
	if err := os.WriteFile(patchPath, patch, 0o644); err != nil {
		return fmt.Errorf("provision: chat profile patch write: %w", err)
	}
	return nil
}

// dropPatchPlugin убирает из cordis.patch.yml записи плагина с именем bundle
// (YAML-список записей {id, name, …}); без таких записей патч не меняется.
func dropPatchPlugin(patch []byte, bundle string) ([]byte, error) {
	if !bytes.Contains(patch, []byte(bundle)) {
		return patch, nil
	}
	var entries []map[string]any
	if err := yaml.Unmarshal(patch, &entries); err != nil {
		return nil, fmt.Errorf("provision: chat profile patch parse: %w", err)
	}
	kept := entries[:0]
	for _, e := range entries {
		if name, _ := e["name"].(string); name == bundle {
			continue
		}
		kept = append(kept, e)
	}
	out, err := yaml.Marshal(kept)
	if err != nil {
		return nil, fmt.Errorf("provision: chat profile patch: %w", err)
	}
	return out, nil
}

const (
	cabinetBundle          = "@arijjia/agctl-cabinet"
	chatSessionFilesBundle = "@arijjia/agctl-chat-session-files"
	chatBrandBundle        = "@arijjia/agctl-chat-brand"
	workspaceConsoleBundle = "@arijjia/agctl-workspace-console"
)

// pruneStaleBundleFiles удаляет файлы в dst, которых нет в скелете src.
// Сгенерированный корневой client.js (compat entry) пропускается. Пустые
// каталоги после удаления не собираются — это безвредно для node_modules.
func pruneStaleBundleFiles(dst, src string, skelFS fs.FS) error {
	return filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		if filepath.ToSlash(rel) == "client.js" {
			return nil // сгенерированный compat entry
		}
		if _, statErr := fs.Stat(skelFS, src+"/"+filepath.ToSlash(rel)); os.IsNotExist(statErr) {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		return nil
	})
}

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

// providerBlock — один провайдер в settings.yaml dsh.
type providerBlock struct {
	alias        string
	keyEnv       string
	baseURL      string
	models       []string
	defaultModel string
	primary      bool
}

// providerBlocks собирает основной провайдер (со всеми BaseURL-алиасами) и
// дополнительные. Алиасы уникальны и безопасны как YAML-ключи.
func providerBlocks(p Provider) []providerBlock {
	used := map[string]bool{}
	unique := func(raw string) string {
		base := providerAlias(raw, "provider")
		alias := base
		for i := 2; used[alias]; i++ {
			alias = base + "-" + strconv.Itoa(i)
		}
		used[alias] = true
		return alias
	}
	var out []providerBlock
	urls := p.AllBaseURLs()
	if len(urls) == 0 {
		urls = []string{""} // прежний пустой-provider/modal flow
	}
	for i, baseURL := range urls {
		rawAlias := p.Alias()
		if i > 0 {
			rawAlias += "-" + strconv.Itoa(i+1)
		}
		out = append(out, providerBlock{
			alias: unique(rawAlias), keyEnv: "AGCTL_LLM_API_KEY", baseURL: baseURL,
			models: p.AllModels(), defaultModel: p.DefaultModel(), primary: i == 0,
		})
	}
	for _, extra := range p.Extras {
		models := extra.AllModels()
		baseURL := strings.TrimRight(strings.TrimSpace(extra.BaseURL), "/")
		if baseURL == "" || len(models) == 0 {
			continue
		}
		out = append(out, providerBlock{
			alias: unique(extra.Alias()), keyEnv: extra.KeyEnv(), baseURL: baseURL,
			models: models, defaultModel: extra.DefaultModel(),
		})
	}
	return out
}

// credentialRefs — упорядоченные пары env → ключ (основной, затем доп.).
func credentialRefs(p Provider) [][2]string {
	seen := map[string]bool{}
	var out [][2]string
	add := func(env, key string) {
		if key == "" || env == "" || seen[env] {
			return
		}
		seen[env] = true
		out = append(out, [2]string{env, key})
	}
	add("AGCTL_LLM_API_KEY", p.APIKey)
	for _, extra := range p.Extras {
		add(extra.KeyEnv(), extra.APIKey)
	}
	return out
}

// syncLLMInPatch приводит llm-pi-ai/agent-default-model в profiles/web/cordis.patch.yml
// к актуальному набору провайдеров. DSH импортирует settings.yaml в этот патч
// слиянием, поэтому старый alias (например, agctl до появления provider_name)
// иначе остаётся в выпадающем списке моделей рядом с новым.
func syncLLMInPatch(home string, blocks []providerBlock, defaultAlias, defaultModel string) error {
	patchPath := filepath.Join(home, "profiles", "web", "cordis.patch.yml")
	raw, err := os.ReadFile(patchPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("provision: provider patch read %s: %w", patchPath, err)
	}
	var entries []map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("provision: provider patch parse %s: %w", patchPath, err)
		}
	}
	providers := make(map[string]any, len(blocks))
	for _, block := range blocks {
		models := make([]any, 0, len(block.models))
		for _, m := range block.models {
			models = append(models, map[string]any{
				"id": m, "name": m, "contextWindow": 262144, "maxTokens": 65536,
			})
		}
		providers[block.alias] = map[string]any{
			"apiKeyEnv": block.keyEnv,
			"api":       "openai-completions",
			"baseURL":   block.baseURL,
			"models":    models,
		}
	}
	const (
		llmPlugin     = "@deepseek-ai/dsh-llm-pi-ai"
		defaultPlugin = "@deepseek-ai/dsh-agent-default-model"
	)
	setConfig := func(entry map[string]any, key string, value any) {
		cfg, _ := entry["config"].(map[string]any)
		if cfg == nil {
			cfg = map[string]any{}
		}
		cfg[key] = value
		entry["config"] = cfg
	}
	haveLLM, haveDefault := false, false
	for _, entry := range entries {
		switch name, _ := entry["name"].(string); name {
		case llmPlugin:
			setConfig(entry, "providers", providers)
			haveLLM = true
		case defaultPlugin:
			setConfig(entry, "provider", defaultAlias)
			setConfig(entry, "model", defaultModel)
			haveDefault = true
		}
	}
	if !haveLLM {
		entries = append(entries, map[string]any{
			"id": "llm-pi-ai", "name": llmPlugin,
			"config": map[string]any{"providers": providers},
		})
	}
	if !haveDefault {
		entries = append(entries, map[string]any{
			"id": "agent-default-model", "name": defaultPlugin,
			"config": map[string]any{"provider": defaultAlias, "model": defaultModel},
		})
	}
	out, err := yaml.Marshal(entries)
	if err != nil {
		return fmt.Errorf("provision: provider patch marshal: %w", err)
	}
	header := []byte("# agctl: провайдеры LLM синхронизируются с профилем при каждом старте.\n")
	if err := os.WriteFile(patchPath, append(header, out...), 0o644); err != nil {
		return fmt.Errorf("provision: provider patch write %s: %w", patchPath, err)
	}
	return nil
}

// RefreshProvider — перезапись настроек провайдера в существующем DSH_HOME
// (ротация ключа, смена Base URL/моделей). Сессии и профили не трогает:
// dsh импортирует свежий settings.yaml при следующем старте.
func RefreshProvider(home string, p Provider) error {
	blocks := providerBlocks(p)
	var sb strings.Builder
	sb.WriteString("# Настройки воркспейса agctl: провайдеры в выпадающем списке dsh.\nllm-pi-ai:\n  providers:\n")
	for _, block := range blocks {
		sb.WriteString("    " + block.alias + ":\n      apiKeyEnv: " + block.keyEnv + "\n      api: openai-completions\n")
		sb.WriteString("      baseURL: " + yamlStr(block.baseURL) + "\n      models:\n")
		for _, m := range block.models {
			sb.WriteString("        - id: " + yamlStr(m) + "\n          name: " + yamlStr(m) +
				"\n          contextWindow: 262144\n          maxTokens: 65536\n")
		}
	}
	defaultAlias, defaultModel := p.Alias(), p.DefaultModel()
	for _, block := range blocks {
		if block.primary {
			defaultAlias, defaultModel = block.alias, block.defaultModel
			break
		}
	}
	sb.WriteString("agent-default-model:\n  provider: " + defaultAlias + "\n  model: " + yamlStr(defaultModel) + "\n")
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("provision: settings.yaml: %w", err)
	}
	if err := syncLLMInPatch(home, blocks, defaultAlias, defaultModel); err != nil {
		return err
	}
	// Пустой токен — не пишем ключ вовсе: dsh-credentials-local падает на
	// пустом значении («remove the key instead») и показывает свой модал
	// ввода API-ключа (паттерн общего чата Tier 0).
	creds := "version: 1\nrefs: {}\n"
	if refs := credentialRefs(p); len(refs) > 0 {
		var cb strings.Builder
		cb.WriteString("version: 1\nrefs:\n")
		for _, ref := range refs {
			cb.WriteString("  " + ref[0] + ": " + yamlStr(ref[1]) + "\n")
		}
		creds = cb.String()
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
