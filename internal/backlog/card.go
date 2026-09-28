// Package backlog — CLI ведения бэклогов формата v1 (schema 1.0.0):
// файл-на-задачу в backlog/open|done, yaml-шапка, append-only журнал.
// Формат совместим с gitctl-экосистемой (kb/dsh): тот же парсер должен
// читать карточки agctl и gitctl одинаково.
package backlog

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Schema — текущая версия схемы карточек.
const Schema = "1.0.0"

// Статусы/приоритеты/размеры — домен формата v1.
var (
	Statuses   = []string{"open", "in-progress", "done", "cancelled"}
	Priorities = []string{"P1", "P2", "P3"}
	Sizes      = []string{"S", "M", "L"}
)

// Card — yaml-шапка карточки.
type Card struct {
	Schema   string   `yaml:"schema"`
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Status   string   `yaml:"status"`
	Priority string   `yaml:"priority"`
	Size     string   `yaml:"size"`
	Area     string   `yaml:"area"`
	Repo     string   `yaml:"repo"`
	Created  string   `yaml:"created"`
	Updated  string   `yaml:"updated"`
	Tags     []string `yaml:"tags,omitempty"`
	Depends  []string `yaml:"depends,omitempty"`
}

// File — карточка целиком: шапка + тело.
type File struct {
	Card Card
	Body string // всё после закрывающего "---" (включая ведущий \n)
	Path string
}

var slugRe = regexp.MustCompile(`[^\p{L}\p{N}]+`)

// Slug — из заголовка: unicode-буквы/цифры в нижнем регистре, остальное — «-».
func Slug(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "task"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// Parse разбирает содержимое карточки (yaml-шапка между --- ---).
func Parse(path, content string) (File, error) {
	f := File{Path: path}
	rest := content
	if strings.HasPrefix(rest, "---\n") || strings.HasPrefix(rest, "---\r\n") {
		idx := strings.Index(rest[4:], "\n---")
		if idx < 0 {
			return f, fmt.Errorf("%s: не найден закрывающий ---", path)
		}
		header := rest[4 : 4+idx]
		f.Body = rest[4+idx+4:]
		if err := yaml.Unmarshal([]byte(header), &f.Card); err != nil {
			return f, fmt.Errorf("%s: yaml-шапка: %w", path, err)
		}
	} else {
		return f, fmt.Errorf("%s: нет yaml-шапки", path)
	}
	return f, nil
}

// Render собирает карточку обратно (шапка + тело).
func (f File) Render() (string, error) {
	head, err := yaml.Marshal(f.Card)
	if err != nil {
		return "", err
	}
	return "---\n" + string(head) + "---" + f.Body, nil
}

// Validate — форматные проверки карточки (для `check`).
func (f File) Validate() []string {
	var errs []string
	c := f.Card
	base := filepath.Base(f.Path)
	want := datePrefix(base) + c.ID + ".md"
	if want != base {
		errs = append(errs, fmt.Sprintf("%s: имя файла не <дата>-%s.md", base, c.ID))
	}
	if c.Schema != Schema {
		errs = append(errs, fmt.Sprintf("%s: schema=%q, ждали %q", base, c.Schema, Schema))
	}
	if !contains(Statuses, c.Status) {
		errs = append(errs, fmt.Sprintf("%s: status=%q недопустим", base, c.Status))
	}
	if !contains(Priorities, c.Priority) {
		errs = append(errs, fmt.Sprintf("%s: priority=%q недопустим", base, c.Priority))
	}
	if !contains(Sizes, c.Size) {
		errs = append(errs, fmt.Sprintf("%s: size=%q недопустим", base, c.Size))
	}
	if c.Title == "" {
		errs = append(errs, fmt.Sprintf("%s: пустой title", base))
	}
	for _, d := range []string{c.Created, c.Updated} {
		if _, err := time.Parse("2006-01-02", d); err != nil {
			errs = append(errs, fmt.Sprintf("%s: дата %q не YYYY-MM-DD", base, d))
		}
	}
	return errs
}

func datePrefix(name string) string {
	if len(name) >= 11 && name[4] == '-' && name[7] == '-' {
		return name[:11] // YYYY-MM-DD-
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Root ищет корень бэклога: от cwd вверх до каталога, содержащего backlog/open.
func Root(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		cand := filepath.Join(dir, "backlog")
		if st, err := os.Stat(filepath.Join(cand, "open")); err == nil && st.IsDir() {
			return cand, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("backlog/ не найден: поднимитесь из репозитория или укажите --root")
		}
		dir = parent
	}
}

// LoadAll читает все карточки open/ и done/.
func LoadAll(root string) ([]File, error) {
	var out []File
	for _, sub := range []string{"open", "done"} {
		dir := filepath.Join(root, sub)
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			f, err := Parse(p, string(b))
			if err != nil {
				return nil, err
			}
			out = append(out, f)
		}
	}
	sortCards(out)
	return out, nil
}

func sortCards(fs []File) {
	pri := map[string]int{"P1": 0, "P2": 1, "P3": 2}
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i].Card, fs[j].Card
		if pri[a.Priority] != pri[b.Priority] {
			return pri[a.Priority] < pri[b.Priority]
		}
		return a.Created+" "+a.ID > b.Created+" "+b.ID
	})
}

// AppendJournal дописывает строку в раздел «## Журнал» (создаёт при отсутствии).
func AppendJournal(body, line string) string {
	const header = "## Журнал"
	idx := strings.Index(body, header)
	if idx < 0 {
		return body + "\n" + header + "\n\n- " + line + "\n"
	}
	// конец раздела — следующий «## » или конец файла
	rest := body[idx+len(header):]
	next := strings.Index(rest, "\n## ")
	entry := "\n- " + line
	if next < 0 {
		return body + entry
	}
	pos := idx + len(header) + next
	return body[:pos] + entry + body[pos:]
}
