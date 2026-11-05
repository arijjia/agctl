package provision

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// components.go — каталог компонентов воркспейса-проекта (скиллы, инструменты,
// промпты) и серверное включение/выключение компонентов после клонирования.
//
// Сценарий 1c-vibe: репка-донор клонится всем одинаковая, а «что из неё
// доступно агенту» — решение платформы на сервере, не правка репозитория.
// Выключенный компонент физически переносится в управляемый stash
// `<project>/.agctl/stash/<kind>/<id>` и исключается из git локально — через
// `.git/info/exclude` (рабочее дерево и .gitignore репки не трогаем, autosave
// не создаёт коммитов поверх переключений). Включение возвращает каталог
// обратно. Весь код идемпотентен: повторное применение того же состояния — no-op.

// Component kinds (агентские поверхности, которые видно в консоли).
const (
	ComponentKindSkill = "skill" // skills/<dir>/SKILL.md — процедура работы агента
	// ComponentKindDshSkill — скилл в автозагружаемом корне .dsh/skills (dsh
	// подхватывает его сам, без указаний в AGENTS.md); сюда ставит пакеты
	// платформенного каталога.
	ComponentKindDshSkill = "dshskill"
	ComponentKindTool     = "tool" // scripts/*.py — CLI-инструменты контура
	ComponentKindData     = "data" // scripts/<dir> — демо-данные/фикстуры
	ComponentKindDocs     = "docs" // промпты/инструкции (AGENTS.md) — только просмотр
)

// ProjectComponent — единица каталога воркспейса для консоли.
type ProjectComponent struct {
	ID          string `json:"id"`   // "<kind>:<name>" — стабильный ключ выбора
	Kind        string `json:"kind"` // skill | tool | data | docs
	Name        string `json:"name"` // имя каталога/файла без kind-префикса
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path"`    // путь в воркспейсе («skills/1c-vibe-demo»)
	Enabled     bool   `json:"enabled"` // false = ушел в stash, агенту не виден
	Locked      bool   `json:"locked"`  // true = переключать нельзя (базовый промпт)
}

// stashRel — управляемый stash внутри воркспейса; в git не попадает: при
// autosave паттерн добавляется в wt.Excludes (StashIgnorePattern), для
// настоящего git — ещё и в .git/info/exclude (ensureProjectStashExcluded).
var stashRel = filepath.Join(".agctl", "stash")

// StashIgnorePattern — gitignore-паттерн stash'а выключенных компонентов.
// Без ведущего слэша: настоящие git и go-git матчат его и на сам каталог,
// и на содержимое (anchored `/.agctl/` go-git на глубокие файлы не
// распространяет).
var StashIgnorePattern = gitignore.ParsePattern(".agctl/", nil)

// scanSkillDir — общий обход корня скиллов (skills/ и .dsh/skills/): каталоги
// с SKILL.md становятся компонентами указанного kind.
func scanSkillDir(dir, root, kind string) []ProjectComponent {
	var out []ProjectComponent
	if entries, err := os.ReadDir(filepath.Join(dir, root)); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := entry.Name()
			comp := ProjectComponent{
				ID: kind + ":" + name, Kind: kind, Name: name,
				Path: root + "/" + name, Enabled: true,
			}
			comp.Title, comp.Description = readSkillMeta(filepath.Join(dir, root, name, "SKILL.md"), name)
			out = append(out, comp)
		}
	}
	return out
}

// ScanProjectComponents читает каталог компонентов из рабочего дерева
// проекта: skills/*/SKILL.md, .dsh/skills/*/SKILL.md (автозагрузка dsh),
// scripts/*.py, каталоги данных scripts и базовые инструкции. dir уже
// провалидирован вызывающим (ProjectDirectory).
func ScanProjectComponents(dir string) []ProjectComponent {
	var out []ProjectComponent
	out = append(out, scanSkillDir(dir, "skills", ComponentKindSkill)...)
	out = append(out, scanSkillDir(dir, ".dsh/skills", ComponentKindDshSkill)...)
	scriptsDir := filepath.Join(dir, "scripts")
	if entries, err := os.ReadDir(scriptsDir); err == nil {
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			name := entry.Name()
			switch {
			case !entry.IsDir() && strings.HasSuffix(name, ".py"):
				comp := ProjectComponent{
					ID: ComponentKindTool + ":" + name, Kind: ComponentKindTool, Name: name,
					Title: strings.TrimSuffix(name, ".py"), Path: "scripts/" + name, Enabled: true,
				}
				comp.Description = readToolDoc(filepath.Join(scriptsDir, name))
				out = append(out, comp)
			case entry.IsDir() && name != "tests" && name != "__pycache__":
				out = append(out, ProjectComponent{
					ID: ComponentKindData + ":" + name, Kind: ComponentKindData, Name: name,
					Title: name, Path: "scripts/" + name, Enabled: true,
				})
			}
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "AGENTS.md")); err == nil {
		out = append(out, ProjectComponent{
			ID: ComponentKindDocs + ":AGENTS.md", Kind: ComponentKindDocs, Name: "AGENTS.md",
			Title: "Инструкции агента (AGENTS.md)", Path: "AGENTS.md",
			Enabled: true, Locked: true,
			Description: "Базовый системный промпт воркспейса; редактируется в репке, из консоли не выключается.",
		})
	}
	// Компоненты, сейчас живущие в stash: видны в каталоге как выключенные.
	// Path всегда показывает каноническое место в дереве (куда вернётся).
	// В stash лежат и каталоги (скиллы/данные), и файлы (инструменты .py).
	stashRoot := filepath.Join(dir, stashRel)
	_ = filepath.WalkDir(stashRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // stash отсутствует/нечитаем — просто нет выключенных
		}
		rel, relErr := filepath.Rel(stashRoot, path)
		if relErr != nil || rel == "." {
			return nil
		}
		parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
		if len(parts) != 2 {
			return nil // уровень kind — идут только листья
		}
		kind, name := parts[0], parts[1]
		id := kind + ":" + name
		for _, comp := range out {
			if comp.ID == id {
				return fs.SkipDir // уже найден в рабочем дереве — не должно быть
			}
		}
		comp := ProjectComponent{ID: id, Kind: kind, Name: name, Path: treeRelPath(kind, name), Enabled: false}
		switch kind {
		case ComponentKindSkill, ComponentKindDshSkill:
			comp.Title, comp.Description = readSkillMeta(filepath.Join(path, "SKILL.md"), name)
		case ComponentKindTool:
			comp.Title = strings.TrimSuffix(name, ".py")
			comp.Description = readToolDoc(filepath.Join(path, name))
		default:
			comp.Title = name
		}
		out = append(out, comp)
		return fs.SkipDir // не спускаться внутрь компонента (безвредно для файла)
	})
	sortComponents(out)
	return out
}

// treeRelPath — канонический путь компонента в рабочем дереве.
func treeRelPath(kind, name string) string {
	switch kind {
	case ComponentKindTool, ComponentKindData:
		return "scripts/" + name
	case ComponentKindDocs:
		return name
	case ComponentKindDshSkill:
		return ".dsh/skills/" + name
	default: // skill и всё неизвестное живёт в skills/
		return "skills/" + name
	}
}

// sortComponents — детерминированный порядок: kind, затем имя.
func sortComponents(comps []ProjectComponent) {
	rank := map[string]int{
		ComponentKindDocs: 0, ComponentKindSkill: 1, ComponentKindDshSkill: 1,
		ComponentKindTool: 2, ComponentKindData: 3,
	}
	sort.Slice(comps, func(i, j int) bool {
		if rank[comps[i].Kind] != rank[comps[j].Kind] {
			return rank[comps[i].Kind] < rank[comps[j].Kind]
		}
		return comps[i].Name < comps[j].Name
	})
}

// readSkillMeta парсит шапку SKILL.md (простые строки `name:`/`description:`
// YAML-фронматтера; dsh-скиллы держат плоские однострочные значения).
func readSkillMeta(path, fallback string) (title, description string) {
	title = fallback
	data, err := os.ReadFile(path)
	if err != nil {
		return title, ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(trimmed, "name:"); ok && title == fallback {
			if v := strings.Trim(strings.TrimSpace(value), `"'`); v != "" {
				title = v
			}
		}
		if value, ok := strings.CutPrefix(trimmed, "description:"); ok {
			if v := strings.Trim(strings.TrimSpace(value), `"'`); v != "" {
				description = v
			}
		}
	}
	return title, description
}

// readToolDoc — первая строка docstring python-файла (краткое описание
// инструмента для консоли; многострочность не нужна).
func readToolDoc(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `"""`) || strings.HasPrefix(trimmed, "'''") {
			q := trimmed[:3]
			if rest := strings.TrimPrefix(trimmed, q); strings.HasSuffix(strings.TrimSpace(rest), q) {
				return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), q))
			}
			if i+1 < len(lines) {
				return strings.TrimSpace(lines[i+1])
			}
		}
	}
	return ""
}

// ValidateComponentSelection проверяет запрошенный набор выключенных
// компонентов против текущего каталога: переключать можно только существующее
// и незалоченное. Возвращает ошибки с ID — UI показывает их как есть.
func ValidateComponentSelection(components []ProjectComponent, disabled []string) error {
	byID := make(map[string]ProjectComponent, len(components))
	for _, comp := range components {
		byID[comp.ID] = comp
	}
	seen := map[string]bool{}
	for _, id := range disabled {
		if seen[id] {
			return fmt.Errorf("компонент %s указан дважды", id)
		}
		seen[id] = true
		comp, ok := byID[id]
		if !ok {
			return fmt.Errorf("компонент %s не найден в воркспейсе", id)
		}
		if comp.Locked {
			return fmt.Errorf("компонент %s — базовый и не выключается", id)
		}
	}
	return nil
}

// ApplyProjectComponents приводит рабочее дерево к выбору: выключенные —
// переносит в stash, включённые — возвращает из stash. Повторы безопасны;
// компонент «включён, но в stash отсутствует» — no-op (клон без него).
func ApplyProjectComponents(dir string, disabled []string) error {
	components := ScanProjectComponents(dir)
	off := make(map[string]bool, len(disabled))
	for _, id := range disabled {
		off[id] = true
	}
	if err := ensureProjectStashExcluded(dir); err != nil {
		return err
	}
	for _, comp := range components {
		if comp.Locked {
			continue
		}
		tree := filepath.Join(dir, filepath.FromSlash(treeRelPath(comp.Kind, comp.Name)))
		stash := filepath.Join(dir, stashRel, comp.Kind, comp.Name)
		if !comp.Enabled && !off[comp.ID] {
			// включить: вернуть из stash, если он там лежит
			if _, err := os.Lstat(stash); err == nil {
				if err := stashMove(stash, tree); err != nil {
					return fmt.Errorf("включение %s: %w", comp.ID, err)
				}
			}
			continue
		}
		if comp.Enabled && off[comp.ID] {
			// выключить: спрятать в stash
			if err := stashMove(tree, stash); err != nil {
				return fmt.Errorf("выключение %s: %w", comp.ID, err)
			}
		}
	}
	return nil
}

// stashMove переносит каталог/файл в пределах одного воркспейса (rename той же
// файловой системы). Целевые каталоги создаются, источник обязан существовать.
func stashMove(src, dst string) error {
	if _, err := os.Lstat(src); err != nil {
		if os.IsNotExist(err) {
			return nil // уже не там (повтор/ручная чистка) — идемпотентно
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("место назначения занято: %s", filepath.Base(dst))
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}

// ensureProjectStashExcluded добавляет `.agctl/` в локальный exclude репы
// (.git/info/exclude): stash не коммитится autosave'ом и не попадает в пуш,
// при этом .gitignore самой репки (общий для всех клонов) остаётся чистым.
func ensureProjectStashExcluded(dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, ".agctl")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Join(dir, stashRel), 0o755); err != nil {
			return err
		}
	}
	infoDir := filepath.Join(dir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(infoDir, "exclude")
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Паттерн без ведущего слэша: настоящие git и go-git матчат его и на
	// сам каталог, и на его содержимое (anchored `/.agctl/` go-git на
	// глубокие пути файлов не распространяет).
	const marker = ".agctl/"
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == marker {
			return nil // уже исключён
		}
	}
	var sb strings.Builder
	sb.Write(raw)
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		sb.WriteByte('\n')
	}
	sb.WriteString("# agctl: серверный stash выключенных компонентов (управляет agctl, не редактировать)\n")
	sb.WriteString(marker + "\n")
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// ProjectComponentsState — персистентный выбор пользователя (хранится в
// agctl settings KV; диск — производная). JSON-схема версии 1.
type ProjectComponentsState struct {
	Schema   int      `json:"schema"` // 1
	Disabled []string `json:"disabled"`
}

// EncodeComponentsState — JSON для KV-колонки настроек.
func EncodeComponentsState(state ProjectComponentsState) (string, error) {
	if state.Schema == 0 {
		state.Schema = 1
	}
	if state.Disabled == nil {
		state.Disabled = []string{}
	}
	body, err := json.Marshal(state)
	return string(body), err
}

// DecodeComponentsState — обратно; пустая/битая запись = ничего не выключено.
func DecodeComponentsState(raw string) ProjectComponentsState {
	var state ProjectComponentsState
	if raw == "" || json.Unmarshal([]byte(raw), &state) != nil || state.Schema != 1 {
		return ProjectComponentsState{Schema: 1, Disabled: []string{}}
	}
	if state.Disabled == nil {
		state.Disabled = []string{}
	}
	return state
}
