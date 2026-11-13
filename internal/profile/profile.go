// Package profile — профиль организации: данные (profile.yaml + ассеты),
// которые настраивают брендинг и платформенные дефолты agctl без пересборки
// бинаря. Ядро agctl универсально; вся кастомизация под конкретную
// организацию живёт в профиле (см. agctl-hq/docs/agctl-decomposition-roadmap.md).
//
// Загрузка: --profile <имя> (встроенные профили) или
// --profile-dir <каталог> (дисковый профиль чужой организации). env:
// AGCTL_PROFILE / AGCTL_PROFILE_DIR.
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
	// ExtraProviders — второй и последующие провайдеры в том же списке.
	ExtraProviders []LLMExtraProvider `yaml:"extra_providers"`
}

// Data — разобранный profile.yaml плюс ассеты профиля.
type Data struct {
	Schema int    `yaml:"schema"`
	ID     string `yaml:"id"`

	Brand brand `yaml:"brand"`

	Login struct {
		Subtitle string `yaml:"subtitle"`
	} `yaml:"login"`

	LLM llm `yaml:"llm"`

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
	fsys fs.FS // корень профиля (catalog/, image/, assets/)
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

// LoadDir загружает дисковый профиль: dir/profile.yaml + ассеты.
func LoadDir(dir string) (*Data, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(abs, "profile.yaml"))
	if err != nil {
		return nil, fmt.Errorf("профиль %s: profile.yaml: %w", abs, err)
	}
	d := &Data{}
	if err := yaml.Unmarshal(raw, d); err != nil {
		return nil, fmt.Errorf("profile.yaml: %w", err)
	}
	if err := d.finalize("dir:" + abs); err != nil {
		return nil, err
	}
	d.fsys = os.DirFS(abs)
	if p := d.Brand.Logo; p != "" {
		logo, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(p)))
		if err != nil {
			return nil, fmt.Errorf("логотип %s: %w", p, err)
		}
		d.logo = logo
	}
	return d, nil
}

// Resolve — порядок разрешения профиля: явный каталог (--profile-dir)
// сильнее имени; имя ищется во встроенных профилях, затем в searchDirs
// (обычно ./profiles и $AGCTL_HOME/profiles — там живут профили организаций,
// полученные `make profile-fetch` или положенные вручную). Ошибка
// перечисляет доступные варианты.
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
	d.Source = source
	return nil
}

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
	LLMExtraProviders []LLMExtraProvider // дополнительные провайдеры (без секретов)
	Scenarios         []Scenario         // готовые сценарии диалога создания воркспейса
	DevImages         []string           // каталог образов лаунчера developer'а
	ImageDockerfile   string             // надстройка образа внутри профиля ("" = нет)
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
		FS:                d.fsys,
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
