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

type llm struct {
	DefaultBaseURL string `yaml:"default_base_url"`
	DefaultModel   string `yaml:"default_model"`
	DefaultModels  string `yaml:"default_models"`
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

	// Source — откуда загружен профиль (для логов и тестов).
	Source string `yaml:"-"`

	logo []byte
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
	return loadFS(embedded, filepath.Join("profiles", name), "embedded:"+name)
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

func nonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func loadFS(fsys embed.FS, dir, source string) (*Data, error) {
	raw, err := fsys.ReadFile(filepath.Join(dir, "profile.yaml"))
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
	if p := d.Brand.Logo; p != "" {
		logo, err := fsys.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
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
	d.Source = source
	return nil
}

// View — предвычисленное представление профиля для потребителей: строки для
// HTML/JS-инъекций готовы (base64-логотип, JSON), ничего не парсится на hot path.
type View struct {
	ID               string
	Name             string
	Tagline          string
	Title            string // <title> вкладки
	Letter           string // буква-знак вместо логотипа
	Subtitle         string // подзаголовок страницы входа
	HelpCabinetTitle string // заголовок справки в Кабинете
	MapLLMLabel      string // подпись LLM на физической карте
	JiraExampleURL   string // пример в доке jirapoll
	LogoDataURI      string // data:image/png;base64,… ("" если логотипа нет)
	LLMBaseURL       string
	LLMModel         string
	LLMModels        string

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
		ID:               d.ID,
		Name:             d.Brand.Name,
		Tagline:          d.Brand.Tagline,
		Title:            d.Brand.Title,
		Letter:           d.Brand.Letter,
		Subtitle:         d.Login.Subtitle,
		HelpCabinetTitle: d.Help.CabinetTitle,
		MapLLMLabel:      d.Map.LLMLabel,
		JiraExampleURL:   d.Examples.JiraURL,
		LLMBaseURL:       d.LLM.DefaultBaseURL,
		LLMModel:         d.LLM.DefaultModel,
		LLMModels:        d.LLM.DefaultModels,
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
