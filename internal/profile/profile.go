// Package profile — профиль организации: данные (profile.yaml + ассеты),
// которые настраивают брендинг и платформенные дефолты agctl без пересборки
// бинаря. Ядро agctl универсально; вся кастомизация под конкретную
// организацию живёт в профиле (см. agctl-hq/docs/agctl-decomposition-roadmap.md).
//
// Загрузка: --profile <имя> (встроенные профили) или
// --profile-dir <каталог> (дисковый профиль чужой организации). env:
// AGCTL_PROFILE / AGCTL_PROFILE_DIR.
//
// Наследование: дисковый профиль может начинаться с extends: <базовый> —
// тогда его profile.yaml накладывается на profile.yaml базового (словари
// сливаются, списки и значения заменяются), а файлы (бренд, каталог, образ)
// ищутся сначала у него, потом у базового. Так варианты одной организации
// (контуры, стенды) держат только свои отличия.
package profile

import (
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed all:profiles
var embedded embed.FS

// Schema — единственная поддержанная версия схемы profile.yaml.
const Schema = 1

type brand struct {
	Name    string `yaml:"name"`
	Tagline string `yaml:"tagline"`
	Title   string `yaml:"title"`
	Letter  string `yaml:"letter"`
	Logo    string `yaml:"logo"` // путь внутри профиля, опционально
}

// LLMExtraProvider — дополнительный провайдер в выпадающем списке моделей
// dsh. Ключ в профиль не пишется: APIKeyEnv — имя env-переменной хоста.
type LLMExtraProvider struct {
	Name      string `yaml:"name"`
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	Models    string `yaml:"models"`
	APIKeyEnv string `yaml:"api_key_env"`
}

// Scenario — готовый сценарий рабочего места: кнопка в диалоге создания
// воркспейса, которая клонирует репо-донор (содержимое организации).
type Scenario struct {
	ID          string `yaml:"id" json:"id"`
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description" json:"description"`
	CloneURL    string `yaml:"clone_url" json:"cloneUrl"`
}

// images — образы рабочего места, которые задаёт организация.
type images struct {
	// Dockerfile — надстройка над базовым agctl-dsh (путь внутри профиля):
	// FROM ${BASE} + пакеты организации. Пусто = базовый образ как есть.
	Dockerfile string `yaml:"dockerfile"`
	// Dev — каталог образов лаунчера developer'а (теги или реестр Nexus).
	Dev []string `yaml:"dev"`
}

type llm struct {
	// ProviderName — алиас основного провайдера в dsh (по умолчанию agctl).
	ProviderName   string `yaml:"provider_name"`
	DefaultBaseURL string `yaml:"default_base_url"`
	DefaultModel   string `yaml:"default_model"`
	DefaultModels  string `yaml:"default_models"`
	// SubagentModels — модели для сабагентов через запятую ("модель" или
	// "алиас/модель"): агент может отдавать мелкие задачи быстрой модели.
	SubagentModels string `yaml:"subagent_models"`
	// ExtraProviders — второй и последующие провайдеры в том же списке.
	ExtraProviders []LLMExtraProvider `yaml:"extra_providers"`
}

// Data — разобранный profile.yaml плюс ассеты профиля.
type Data struct {
	Schema int    `yaml:"schema"`
	ID     string `yaml:"id"`

	// Extends — базовый профиль: id соседнего каталога (или каталога рядом
	// с родительским — для варианта внутри репозитория базового) либо путь от
	// этого профиля (".."). После загрузки — как было записано в файле.
	Extends string `yaml:"extends,omitempty"`

	// Env — несекретные значения по умолчанию для рабочего места (адреса
	// Jira, Confluence, GitLab организации): действуют, пока в доме не задано
	// своё. Имена секретов (…_TOKEN, …_KEY, пароли) здесь отклоняются.
	Env map[string]string `yaml:"env"`

	// EnvFrom — где искать значения в окружении хоста: имя в agctl → имена
	// переменных хоста по порядку (например AGCTL_LLM_API_KEY: [LLM_TOKEN]).
	// agctl dev up переносит найденное только в пустые записи дома.
	EnvFrom map[string][]string `yaml:"env_from"`

	Brand brand `yaml:"brand"`

	Login struct {
		Subtitle string `yaml:"subtitle"`
	} `yaml:"login"`

	// Install — откуда разработчик ставит agctl с этим профилем (страница
	// «Скачать»): репозиторий GitHub с комплектами организации.
	Install struct {
		Repo string `yaml:"repo"`
	} `yaml:"install"`

	LLM llm `yaml:"llm"`

	// Egress — куда контейнеру рабочего места можно ходить при включённом
	// egress-allowlist (карточка dockerrt-egress-flag-key-proxy): хосты,
	// IPv4 или CIDR (GitLab, Jira, реестр). Хосты провайдеров модели
	// разрешаются всегда, их сюда писать не нужно.
	Egress struct {
		Allow []string `yaml:"allow"`
	} `yaml:"egress"`

	// TLS — доверие рабочего места к серверам организации: CAHost — брать
	// доверенные сертификаты машины разработчика (там уже стоят сертификаты
	// организации), CAFiles — корневые сертификаты PEM внутри профиля.
	TLS struct {
		CAHost  bool     `yaml:"ca_host"`
		CAFiles []string `yaml:"ca_files"`
	} `yaml:"tls"`

	Help struct {
		CabinetTitle string `yaml:"cabinet_title"`
	} `yaml:"help"`

	Map struct {
		LLMLabel string `yaml:"llm_label"`
	} `yaml:"map"`

	Examples struct {
		JiraURL string `yaml:"jira_url"`
	} `yaml:"examples"`

	// Workspace — содержимое организации для рабочих мест: готовые
	// сценарии. Пакеты возможностей лежат файлами в catalog/<id>/ профиля.
	Workspace struct {
		Scenarios []Scenario `yaml:"scenarios"`
	} `yaml:"workspace"`

	Images images `yaml:"images"`

	// Source — откуда загружен профиль (для логов и тестов).
	Source string `yaml:"-"`

	logo []byte
	fsys fs.FS  // корень профиля (catalog/, image/, assets/)
	dir  string // каталог дискового профиля ("" — встроенный)
}

// EmbeddedNames — имена профилей, встроенных в бинарь.
func EmbeddedNames() []string {
	entries, err := fs.ReadDir(embedded, "profiles")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// Embedded загружает профиль, встроенный в бинарь (profiles/<name>/).
func Embedded(name string) (*Data, error) {
	// Пути в embed.FS всегда через «/»: filepath.Join на Windows даёт «\»,
	// и embed не находит profiles\agctl\profile.yaml.
	return loadFS(embedded, "profiles/"+name, "embedded:"+name)
}

// LoadDir загружает дисковый профиль: dir/profile.yaml + ассеты; с
// extends — поверх базового профиля (см. описание пакета).
func LoadDir(dir string) (*Data, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(abs, "profile.yaml"))
	if err != nil {
		return nil, fmt.Errorf("профиль %s: profile.yaml: %w", abs, err)
	}
	d, layers, err := decodeChain(abs, raw)
	if err != nil {
		return nil, err
	}
	if err := d.finalize("dir:" + abs); err != nil {
		return nil, err
	}
	d.fsys = layers
	d.dir = abs
	if p := d.Brand.Logo; p != "" {
		logo, err := fs.ReadFile(layers, p)
		if err != nil {
			return nil, fmt.Errorf("логотип %s: %w", p, err)
		}
		d.logo = logo
	}
	return d, nil
}

// maxExtendsDepth — предел цепочки наследования (защита от циклов через
// симлинки, которые не видны по путям).
const maxExtendsDepth = 8

// decodeChain — profile.yaml каталога abs (текст raw) с учётом extends:
// разобранный слитый профиль и файлы слоёв (потомок первым). Extends в
// результате — как записано у самого профиля.
func decodeChain(abs string, raw []byte) (*Data, layeredFS, error) {
	merged, dirs, err := mergeChain(abs, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, nil, err
	}
	d := &Data{}
	if err := yaml.Unmarshal(out, d); err != nil {
		return nil, nil, fmt.Errorf("profile.yaml: %w", err)
	}
	var own struct {
		Extends string `yaml:"extends"`
	}
	_ = yaml.Unmarshal(raw, &own)
	d.Extends = strings.TrimSpace(own.Extends)
	layers := make(layeredFS, 0, len(dirs))
	for _, dir := range dirs {
		layers = append(layers, os.DirFS(dir))
	}
	return d, layers, nil
}

// mergeChain — profile.yaml как словарь, слитый от самого базового профиля
// к abs, и каталоги цепочки (abs первым).
func mergeChain(abs string, raw []byte, seen []string) (map[string]any, []string, error) {
	for _, s := range seen {
		if s == abs {
			return nil, nil, fmt.Errorf("extends: цикл %s → %s", strings.Join(seen, " → "), abs)
		}
	}
	if len(seen) >= maxExtendsDepth {
		return nil, nil, fmt.Errorf("extends: цепочка длиннее %d профилей", maxExtendsDepth)
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", filepath.Join(abs, "profile.yaml"), err)
	}
	ext, _ := m["extends"].(string)
	delete(m, "extends")
	if strings.TrimSpace(ext) == "" {
		return m, []string{abs}, nil
	}
	baseDir, err := extendsDir(abs, strings.TrimSpace(ext))
	if err != nil {
		return nil, nil, err
	}
	baseRaw, err := os.ReadFile(filepath.Join(baseDir, "profile.yaml"))
	if err != nil {
		return nil, nil, fmt.Errorf("extends %q: %w", ext, err)
	}
	base, dirs, err := mergeChain(baseDir, baseRaw, append(seen, abs))
	if err != nil {
		return nil, nil, err
	}
	return mergeMaps(base, m), append([]string{abs}, dirs...), nil
}

// extendsDir — каталог базового профиля. Путь (с «/» или с «.») — от
// каталога профиля; id — соседний каталог, а для варианта внутри
// репозитория базового (<профили>/org/dev, extends: org) — каталог рядом
// с родительским.
func extendsDir(abs, ext string) (string, error) {
	var cands []string
	if strings.ContainsAny(ext, `/\`) || strings.HasPrefix(ext, ".") {
		cands = []string{filepath.Join(abs, filepath.FromSlash(ext))}
	} else {
		parent := filepath.Dir(abs)
		cands = []string{filepath.Join(parent, ext), filepath.Join(filepath.Dir(parent), ext)}
	}
	for _, c := range cands {
		if _, err := os.Stat(filepath.Join(c, "profile.yaml")); err == nil {
			return filepath.Clean(c), nil
		}
	}
	return "", fmt.Errorf("extends %q: базовый профиль не найден (искал: %s)", ext, strings.Join(cands, ", "))
}

// mergeMaps — base, поверх которого лежит over: вложенные словари сливаются,
// списки и значения over заменяют базовые целиком.
func mergeMaps(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = mergeMaps(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}

// layeredFS — файлы профиля с наследованием: файл потомка закрывает файл
// базового с тем же путём, содержимое каталогов объединяется.
type layeredFS []fs.FS

func (l layeredFS) Open(name string) (fs.File, error) {
	var first error
	for _, f := range l {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
		if first == nil {
			first = err
		}
	}
	if first == nil {
		first = &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return nil, first
}

func (l layeredFS) ReadDir(name string) ([]fs.DirEntry, error) {
	seen := map[string]bool{}
	var out []fs.DirEntry
	found := false
	var last error
	for _, f := range l {
		ents, err := fs.ReadDir(f, name)
		if err != nil {
			last = err
			continue
		}
		found = true
		for _, e := range ents {
			if !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e)
			}
		}
	}
	if !found {
		if last == nil {
			last = &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
		}
		return nil, last
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// Resolve — порядок разрешения профиля: явный каталог (--profile-dir)
// сильнее имени; имя ищется во встроенных профилях, затем в searchDirs
// (сервер организации: ./profiles и $AGCTL_HOME/profiles). Локальный режим
// сюда не ходит: у него свой порядок — localmode.ResolveProfile
// (profile_dir > <корень>/profiles/<id> > встроенный). Ошибка перечисляет
// доступные варианты.
func Resolve(name, dir string, search ...string) (*Data, error) {
	if dir != "" {
		return LoadDir(dir)
	}
	if name == "" {
		name = "agctl"
	}
	if d, err := Embedded(name); err == nil {
		return d, nil
	}
	for _, base := range search {
		if strings.TrimSpace(base) == "" {
			continue
		}
		cand := filepath.Join(base, name)
		if _, err := os.Stat(filepath.Join(cand, "profile.yaml")); err == nil {
			return LoadDir(cand)
		}
	}
	return nil, fmt.Errorf("профиль %q не найден: встроенные: %s; дисковые каталоги: %s (свой каталог — --profile-dir)",
		name, strings.Join(EmbeddedNames(), ", "), strings.Join(nonEmpty(search), ", "))
}

// nonEmpty фильтрует пустые и пробельные строки из списка.
func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// loadFS читает профиль из embed-FS: profile.yaml + опциональный логотип
// (путь из brand.logo, относительно каталога профиля).
func loadFS(fsys embed.FS, dir, source string) (*Data, error) {
	// dir и логотип всегда со слэшами: embed.FS не понимает «\» (windows).
	raw, err := fsys.ReadFile(dir + "/profile.yaml")
	if err != nil {
		return nil, fmt.Errorf("profile.yaml: %w", err)
	}
	d := &Data{}
	if err := yaml.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("profile.yaml: %w", err)
	}
	if err := d.finalize(source); err != nil {
		return nil, err
	}
	if sub, err := fs.Sub(fsys, dir); err == nil {
		d.fsys = sub
	}
	if p := d.Brand.Logo; p != "" {
		logo, err := fsys.ReadFile(dir + "/" + p)
		if err != nil {
			return nil, fmt.Errorf("логотип %s: %w", p, err)
		}
		d.logo = logo
	}
	return d, nil
}

// finalize валидирует и проставляет дефолты, зависящие от других полей.
func (d *Data) finalize(source string) error {
	if d.Schema != Schema {
		return fmt.Errorf("schema: поддерживается %d, в файле %d", Schema, d.Schema)
	}
	if d.ID == "" {
		return fmt.Errorf("id: пустой")
	}
	if d.Brand.Name == "" {
		return fmt.Errorf("brand.name: пустой")
	}
	if d.Brand.Title == "" {
		d.Brand.Title = d.Brand.Name
	}
	if d.Brand.Letter == "" {
		d.Brand.Letter = firstGrapheme(d.Brand.Name)
	}
	seen := map[string]bool{}
	for i, sc := range d.Workspace.Scenarios {
		if sc.ID == "" || sc.Title == "" || sc.CloneURL == "" {
			return fmt.Errorf("workspace.scenarios[%d]: нужны id, title и clone_url", i)
		}
		if seen[sc.ID] {
			return fmt.Errorf("workspace.scenarios: повтор id %q", sc.ID)
		}
		seen[sc.ID] = true
	}
	if p := d.Images.Dockerfile; p != "" && (strings.HasPrefix(p, "/") || strings.Contains(p, "..")) {
		return fmt.Errorf("images.dockerfile: путь внутри профиля, без .. и абсолютных путей: %q", p)
	}
	for _, p := range d.TLS.CAFiles {
		if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return fmt.Errorf("tls.ca_files: путь внутри профиля, без .. и абсолютных путей: %q", p)
		}
	}
	for k, v := range d.Env {
		if !envNameRe.MatchString(k) {
			return fmt.Errorf("env: имя %q — только A-Z, 0-9 и _, с буквы", k)
		}
		if secretNameRe.MatchString(k) {
			return fmt.Errorf("env: %s похоже на секрет — в профиле только несекретные значения (адреса, логины); секрет — в env_from или в «Доступе»", k)
		}
		if strings.TrimSpace(v) == "" || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("env.%s: нужна одна непустая строка", k)
		}
	}
	for k, srcs := range d.EnvFrom {
		if !envNameRe.MatchString(k) {
			return fmt.Errorf("env_from: имя %q — только A-Z, 0-9 и _, с буквы", k)
		}
		for _, s := range srcs {
			if !envNameRe.MatchString(s) {
				return fmt.Errorf("env_from.%s: имя переменной хоста %q — только A-Z, 0-9 и _, с буквы", k, s)
			}
		}
	}
	d.Source = source
	return nil
}

var (
	envNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	// secretNameRe — имена, под которыми обычно лежат секреты.
	secretNameRe = regexp.MustCompile(`(KEY|TOKEN|SECRET|PASSWORD|PASSWD)$`)
)

// SecretName — имя переменной выглядит как секрет (…_KEY, …_TOKEN, пароли).
func SecretName(name string) bool { return secretNameRe.MatchString(name) }

// View — предвычисленное представление профиля для потребителей: строки для
// HTML/JS-инъекций готовы (base64-логотип, JSON), ничего не парсится на hot path.
type View struct {
	ID                string
	Name              string
	Tagline           string
	Title             string // <title> вкладки
	Letter            string // буква-знак вместо логотипа
	Subtitle          string // подзаголовок страницы входа
	HelpCabinetTitle  string // заголовок справки в Кабинете
	MapLLMLabel       string // подпись LLM на физической карте
	JiraExampleURL    string // пример в доке jirapoll
	LogoDataURI       string // data:image/png;base64,… ("" если логотипа нет)
	LLMProvider       string // алиас основного провайдера (по умолчанию agctl)
	LLMBaseURL        string
	LLMModel          string
	LLMModels         string
	LLMExtraProviders []LLMExtraProvider  // дополнительные провайдеры (без секретов)
	Scenarios         []Scenario          // готовые сценарии диалога создания воркспейса
	DevImages         []string            // каталог образов лаунчера developer'а
	ImageDockerfile   string              // надстройка образа внутри профиля ("" = нет)
	Extends           string              // базовый профиль, как записан в profile.yaml
	Env               map[string]string   // несекретные значения по умолчанию
	EnvFrom           map[string][]string // источники значений в окружении хоста
	// FS — корень профиля: catalog/<id>/ (пакеты возможностей), image/
	// (контекст надстройки образа). nil у профиля без файловой части.
	FS fs.FS

	brandJSON string
}

// Neutral — вью нейтрального встроенного профиля agctl (ядро без организации).
func Neutral() View {
	d, err := Embedded("agctl")
	if err != nil {
		panic("profile: встроенный agctl отсутствует: " + err.Error())
	}
	return d.View()
}

// View предвычисляет производные строки. Большие данные (логотип) кодируются
// один раз здесь.
func (d *Data) View() View {
	v := View{
		ID:                d.ID,
		Name:              d.Brand.Name,
		Tagline:           d.Brand.Tagline,
		Title:             d.Brand.Title,
		Letter:            d.Brand.Letter,
		Subtitle:          d.Login.Subtitle,
		HelpCabinetTitle:  d.Help.CabinetTitle,
		MapLLMLabel:       d.Map.LLMLabel,
		JiraExampleURL:    d.Examples.JiraURL,
		LLMProvider:       d.LLM.ProviderName,
		LLMBaseURL:        d.LLM.DefaultBaseURL,
		LLMModel:          d.LLM.DefaultModel,
		LLMModels:         d.LLM.DefaultModels,
		LLMExtraProviders: append([]LLMExtraProvider(nil), d.LLM.ExtraProviders...),
		Scenarios:         append([]Scenario(nil), d.Workspace.Scenarios...),
		DevImages:         append([]string(nil), d.Images.Dev...),
		ImageDockerfile:   d.Images.Dockerfile,
		Extends:           d.Extends,
		FS:                d.fsys,
	}
	if len(d.Env) > 0 {
		v.Env = make(map[string]string, len(d.Env))
		for k, val := range d.Env {
			v.Env[k] = strings.TrimSpace(val)
		}
	}
	if len(d.EnvFrom) > 0 {
		v.EnvFrom = make(map[string][]string, len(d.EnvFrom))
		for k, srcs := range d.EnvFrom {
			v.EnvFrom[k] = append([]string(nil), srcs...)
		}
	}
	if len(d.logo) > 0 {
		v.LogoDataURI = "data:image/png;base64," + base64.StdEncoding.EncodeToString(d.logo)
	}
	j, err := json.Marshal(struct {
		Name     string `json:"name,omitempty"`
		Tagline  string `json:"tagline,omitempty"`
		Title    string `json:"title,omitempty"`
		Letter   string `json:"letter,omitempty"`
		Subtitle string `json:"subtitle,omitempty"`
		Logo     string `json:"logo,omitempty"`
		Help     string `json:"helpTitle,omitempty"`
	}{
		Name: v.Name, Tagline: v.Tagline, Title: v.Title, Letter: v.Letter,
		Subtitle: v.Subtitle, Logo: v.LogoDataURI, Help: v.HelpCabinetTitle,
	})
	if err != nil {
		// json.Marshal на строках не падает; страхуемся нейтральным минимумом.
		j = []byte(`{}`)
	}
	v.brandJSON = string(j)
	return v
}

// BrandJSON — компактный JSON брендинга для window.__AGCTL_BRAND__.
// HTML-символы экранированы json.Marshal'ом, поэтому инлайн в <script> безопасен.
func (v View) BrandJSON() string { return v.brandJSON }

// BrandBootScript — тег для инъекции в <head> до загрузки SPA и плагинов.
func (v View) BrandBootScript() string {
	return `<script data-agctl="brand-boot">window.__AGCTL_BRAND__=` + v.brandJSON + `;</script>`
}

// firstGrapheme — первая буква имени для буквы-знака по умолчанию
// (рунный шаг корректен для кириллицы/латиницы).
func firstGrapheme(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)[0]
	return strings.ToUpper(string(r))
}

// OverlayImage — тег образа организации поверх базового agctl-dsh:
// agctl-dsh:0.2.2 → agctl-dsh-<id>:0.2.2 (реестр и digest сохраняются как
// есть: reg:5000/agctl-dsh:1 → reg:5000/agctl-dsh-<id>:1).
func OverlayImage(base, id string) string {
	if id == "" {
		return base
	}
	slash := strings.LastIndex(base, "/")
	name, tag := base, ""
	if i := strings.LastIndex(base, ":"); i > slash {
		name, tag = base[:i], base[i:]
	}
	return name + "-" + id + tag
}

// ImageContext — контекст сборки надстройки: каталог Dockerfile внутри
// профиля и имя файла. ok=false, если надстройки нет.
func (v View) ImageContext() (ctx fs.FS, dockerfile string, ok bool) {
	if v.ImageDockerfile == "" || v.FS == nil {
		return nil, "", false
	}
	dir, file := path.Split(v.ImageDockerfile)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return v.FS, file, true
	}
	sub, err := fs.Sub(v.FS, dir)
	if err != nil {
		return nil, "", false
	}
	return sub, file, true
}

// SourceDir — каталог профиля на диске; "" — профиль встроен в бинарь и
// из интерфейса не редактируется.
func (d *Data) SourceDir() string {
	if d == nil {
		return ""
	}
	return d.dir
}

// MaxLogoBytes — предел размера логотипа профиля.
const MaxLogoBytes = 512 << 10

// CheckSource проверяет текст profile.yaml для каталога dir теми же
// правилами, что и загрузка: схема, обязательные поля, сценарии, логотип
// на месте. id менять нельзя — на нём держатся имена каталогов и образов.
func CheckSource(dir, wantID string, raw []byte) (*Data, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	d, layers, err := decodeChain(abs, raw)
	if err != nil {
		return nil, err
	}
	if err := d.finalize("dir:" + dir); err != nil {
		return nil, err
	}
	if wantID != "" && d.ID != wantID {
		return nil, fmt.Errorf("id: менять нельзя (сейчас %q) — от него зависят каталог профиля, дом и имена образов", wantID)
	}
	if p := d.Brand.Logo; p != "" {
		if strings.Contains(p, "..") || filepath.IsAbs(p) {
			return nil, fmt.Errorf("brand.logo: путь внутри профиля, без «..»")
		}
		if _, err := fs.Stat(layers, p); err != nil {
			return nil, fmt.Errorf("brand.logo: файла %s нет в профиле", p)
		}
	}
	return d, nil
}

// WriteSource атомарно заменяет profile.yaml каталога dir, оставляя прежний
// текст рядом (profile.yaml.bak). Текст должен пройти CheckSource.
func WriteSource(dir string, raw []byte) error {
	target := filepath.Join(dir, "profile.yaml")
	if prev, err := os.ReadFile(target); err == nil {
		if err := os.WriteFile(target+".bak", prev, 0o644); err != nil {
			return err
		}
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
