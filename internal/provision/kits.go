package provision

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Kit — версионированный стартовый комплект: именованный пресет компонентов.
type Kit struct {
	ID                string         `json:"id"`
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	Version           string         `json:"version"`
	DefaultComponents []string       `json:"defaultComponents"`
	Components        []KitComponent `json:"components"`
}

// KitComponent — компонент, доступный для выбора при сборке конкретного набора.
type KitComponent struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

const kitManifestVersion = "1.0.0"

// Kits — встроенный versioned manifest первой версии каталога.
// Порядок компонентов задаёт детерминированное наложение файлов: предметные
// инструкции могут заменить базовые AGENTS.md/README/.gitignore.
var Kits = []Kit{
	{
		ID: "base", Title: "Базовый комплект", Version: kitManifestVersion,
		Description:       "Минимальная основа рабочего места; компоненты можно отключить.",
		DefaultComponents: []string{"core-workspace", "core-instructions"},
		Components: []KitComponent{
			{ID: "core-workspace", Title: "Память и бэклог", Description: "Git-основа, правила хранения файлов и стартовая карточка задач."},
			{ID: "core-instructions", Title: "Базовые инструкции агента", Description: "AGENTS.md и README для универсального рабочего места."},
		},
	},
	{
		ID: "1c-vibe", Title: "1С-разработка", Version: kitManifestVersion,
		Description:       "Набор типовых компонентов для цикла Jira → правка → проверка → MR.",
		DefaultComponents: []string{"core-workspace", "core-instructions", "1c-instructions", "1c-tooling", "1c-demo-project", "1c-skill"},
		Components: []KitComponent{
			{ID: "core-workspace", Title: "Память и бэклог", Description: "Git-основа и стартовая карточка задач."},
			{ID: "core-instructions", Title: "Базовые инструкции", Description: "Универсальный AGENTS.md и README; может быть заменён 1С-инструкциями."},
			{ID: "1c-instructions", Title: "Правила 1С-контура", Description: "Предметные AGENTS.md, README, backlog и .gitignore."},
			{ID: "1c-tooling", Title: "Инструменты Jira/GitLab/BSL", Description: "Клиенты, сценарии публикации, тесты и конфигурация."},
			{ID: "1c-demo-project", Title: "Демо-проект 1С", Description: "Тестовая конфигурация для локальных проверок без корпоративных доступов."},
			{ID: "1c-skill", Title: "Навык 1C Vibe", Description: "Процедура работы агента с 1С-задачами."},
		},
	},
}

type componentSource struct {
	root  string
	paths []string
}

var componentSources = map[string]componentSource{
	"core-workspace": {
		root: "templates/base", paths: []string{".gitignore", "backlog", "skills/backlog"},
	},
	"core-instructions": {
		root: "templates/base", paths: []string{"AGENTS.md", "README.md"},
	},
	"1c-instructions": {
		root: "templates/1c-vibe", paths: []string{"AGENTS.md", "README.md", ".gitignore", "backlog"},
	},
	"1c-tooling": {
		root: "templates/1c-vibe",
		paths: []string{
			"scripts/.env.example", "scripts/envconfig.py", "scripts/gitlab_client.py",
			"scripts/jira_client.py", "scripts/lint.py", "scripts/main.py",
			"scripts/publish.py", "scripts/pyproject.toml", "scripts/seed_demo.py",
			"scripts/tests",
		},
	},
	"1c-demo-project": {
		root: "templates/1c-vibe", paths: []string{"scripts/demo-project"},
	},
	"1c-skill": {
		root: "templates/1c-vibe", paths: []string{"skills/1c-vibe-demo"},
	},
}

// NormalizeComponents валидирует состав комплекта и возвращает component IDs
// в порядке из manifest, независимо от порядка пользовательского JSON.
// nil означает «комплект по умолчанию»; явный пустой список — ошибка.
func NormalizeComponents(kitID string, requested []string) (string, []string, error) {
	if kitID == "" {
		kitID = "base"
	}
	kit, ok := FindKit(kitID)
	if !ok {
		return "", nil, fmt.Errorf("неизвестный комплект %q", kitID)
	}
	selected := make(map[string]bool)
	if requested == nil {
		for _, id := range kit.DefaultComponents {
			selected[id] = true
		}
	} else {
		for _, id := range requested {
			if selected[id] {
				continue
			}
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return "", nil, fmt.Errorf("выберите хотя бы один компонент")
	}
	allowed := make(map[string]bool, len(kit.Components))
	for _, c := range kit.Components {
		allowed[c.ID] = true
	}
	for id := range selected {
		if !allowed[id] {
			return "", nil, fmt.Errorf("компонент %q не входит в комплект %q", id, kitID)
		}
	}
	ids := make([]string, 0, len(selected))
	for _, c := range kit.Components {
		if selected[c.ID] {
			ids = append(ids, c.ID)
		}
	}
	return kitID, ids, nil
}

// FindKit находит built-in kit по ID.
func FindKit(id string) (Kit, bool) {
	for _, kit := range Kits {
		if kit.ID == id {
			return kit, true
		}
	}
	return Kit{}, false
}

// CopySelectedComponents собирает рабочее дерево из выбранных компонентов.
// Повторяющиеся пути разрешаются порядком manifest (последний overlay побеждает).
func CopySelectedComponents(templateFS fs.FS, kitID string, selected []string, dst string) error {
	kit, ok := FindKit(kitID)
	if !ok {
		return fmt.Errorf("неизвестный комплект %q", kitID)
	}
	_, ids, err := NormalizeComponents(kitID, selected)
	if err != nil {
		return err
	}
	for _, id := range ids {
		var source componentSource
		found := false
		for _, c := range kit.Components {
			if c.ID == id {
				source, found = componentSources[id]
				break
			}
		}
		if !found {
			return fmt.Errorf("нет источника компонента %q", id)
		}
		for _, rel := range source.paths {
			if err := copyComponentPath(templateFS, source.root, rel, dst); err != nil {
				return fmt.Errorf("компонент %s (%s): %w", id, rel, err)
			}
		}
	}
	return nil
}

func copyComponentPath(fsys fs.FS, root, rel, dst string) error {
	source := path.Clean(path.Join(root, rel))
	if !fs.ValidPath(source) {
		return fmt.Errorf("invalid source path %q", source)
	}
	info, err := fs.Stat(fsys, source)
	if err != nil {
		return err
	}
	copyOne := func(src string, entry fs.DirEntry) error {
		relPath := strings.TrimPrefix(strings.TrimPrefix(src, root), "/")
		target := filepath.Join(dst, filepath.FromSlash(relPath))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(fsys, src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}
	if !info.IsDir() {
		return copyOne(source, fs.FileInfoToDirEntry(info))
	}
	return fs.WalkDir(fsys, source, func(src string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return copyOne(src, entry)
	})
}

// kitManifestPath — файл манифеста комплекта внутри рабочей репы.
const kitManifestPath = "agctl-kit.json"

// WriteKitManifest фиксирует выбранный комплект в рабочей репе: состав
// становится воспроизводимым артефактом (audit/rollback), а не ephemeral
// состоянием формы. Вызывать после CopySelectedComponents и до первого коммита.
func WriteKitManifest(dst, kitID string, components []string) error {
	kit, ok := FindKit(kitID)
	if !ok {
		return fmt.Errorf("неизвестный комплект %q", kitID)
	}
	normalizedKit, normalized, err := NormalizeComponents(kitID, components)
	if err != nil {
		return err
	}
	manifest := map[string]any{
		"schema":     kitManifestVersion,
		"kit":        normalizedKit,
		"kitVersion": kit.Version,
		"components": normalized,
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(dst, kitManifestPath)
	return os.WriteFile(target, append(body, '\n'), 0o644)
}
