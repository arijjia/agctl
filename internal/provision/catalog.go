package provision

// catalog.go — платформенный каталог пакетов («возможности воркспейса»):
// скиллы/шаблоны/инструменты, которые пользователь включает в воркспейсе
// одним кликом. Каталог — содержимое организации и живёт в её профиле:
// пакет — каталог catalog/<id> профиля: manifest.json +
// files/**. Файлы ставятся 1:1 в рабочее дерево воркспейса; скиллы — в
// .dsh/skills/<name>/, чтобы dsh подхватывал их автоматически (проектные
// корни скиллов: .dsh/skills и .agents/skills). Один и тот же каталог
// обслуживает и выбор при создании воркспейса, и консоль компонентов —
// единое место правды о возможностях.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CatalogPackage — пакет каталога: метаданные манифеста + список файлов.
type CatalogPackage struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Schema      int      `json:"schema"`
	Files       []string `json:"files"` // относительные пути внутри воркспейса
}

const catalogManifest = "manifest.json"
const catalogFilesDir = "files"

// catalogPrefix — каталог пакетов относительно корня FS профиля.
const catalogPrefix = "catalog"

// CatalogPackages — все пакеты каталога профиля (детерминированный порядок);
// nil FS или профиль без catalog/ — пустой каталог.
func CatalogPackages(catalogFS fs.FS) ([]CatalogPackage, error) {
	if catalogFS == nil {
		return []CatalogPackage{}, nil
	}
	entries, err := fs.ReadDir(catalogFS, catalogPrefix)
	if errors.Is(err, fs.ErrNotExist) {
		return []CatalogPackage{}, nil // у профиля нет своего каталога
	}
	if err != nil {
		return nil, fmt.Errorf("catalog read: %w", err)
	}
	out := []CatalogPackage{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pkg, err := readCatalogPackage(catalogFS, e.Name())
		if err != nil {
			return nil, fmt.Errorf("catalog %s: %w", e.Name(), err)
		}
		out = append(out, pkg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// FindCatalogPackage — пакет по id.
func FindCatalogPackage(catalogFS fs.FS, id string) (CatalogPackage, error) {
	if !validCatalogID(id) {
		return CatalogPackage{}, fmt.Errorf("некорректный id пакета %q", id)
	}
	if catalogFS == nil {
		return CatalogPackage{}, fmt.Errorf("пакет %q: %w", id, fs.ErrNotExist)
	}
	return readCatalogPackage(catalogFS, id)
}

func readCatalogPackage(catalogFS fs.FS, dir string) (CatalogPackage, error) {
	raw, err := fs.ReadFile(catalogFS, catalogPrefix+"/"+dir+"/"+catalogManifest)
	if err != nil {
		return CatalogPackage{}, fmt.Errorf("manifest: %w", err)
	}
	var pkg CatalogPackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return CatalogPackage{}, fmt.Errorf("manifest json: %w", err)
	}
	pkg.Schema = 1
	if pkg.ID == "" {
		pkg.ID = dir
	}
	if !validCatalogID(pkg.ID) {
		return CatalogPackage{}, fmt.Errorf("некорректный id %q в манифесте", pkg.ID)
	}
	files, err := collectCatalogFiles(catalogFS, catalogPrefix+"/"+dir+"/"+catalogFilesDir)
	if err != nil {
		return CatalogPackage{}, err
	}
	pkg.Files = files
	return pkg, nil
}

// collectCatalogFiles — относительные пути файлов пакета (манифест не включает;
// истина о составе — файловая система, чтобы manifest не дрейфовал).
func collectCatalogFiles(catalogFS fs.FS, root string) ([]string, error) {
	var out []string
	err := fs.WalkDir(catalogFS, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return nil // пакет без files — только метаданные (допустимо)
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("files: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

// validCatalogID — id пакета безопасен как имя каталога (один сегмент).
func validCatalogID(id string) bool {
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return false
		}
	}
	return true
}

// InstallCatalogPackage — скопировать файлы пакета в воркспейс (1:1 с files/).
// Существующие файлы перезаписываются (переустановка = обновление до версии
// каталога). Идемпотентно.
func InstallCatalogPackage(dir string, catalogFS fs.FS, pkg CatalogPackage) error {
	for _, rel := range pkg.Files {
		data, err := fs.ReadFile(catalogFS, catalogPrefix+"/"+pkg.ID+"/"+catalogFilesDir+"/"+rel)
		if err != nil {
			return fmt.Errorf("файл %s: %w", rel, err)
		}
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// RemoveCatalogPackage — удалить файлы пакета из воркспейса. Файл, изменённый
// относительно каталога, не удаляется молча: возврат ошибки «изменён» —
// пользователь правит руками (защита от потери работы).
func RemoveCatalogPackage(dir string, catalogFS fs.FS, pkg CatalogPackage) error {
	for _, rel := range pkg.Files {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		raw, err := os.ReadFile(target) // #nosec G304 — dir валидирован вызывающим
		if err != nil {
			if os.IsNotExist(err) {
				continue // уже нет — идемпотентно
			}
			return err
		}
		origin, originErr := fs.ReadFile(catalogFS, catalogPrefix+"/"+pkg.ID+"/"+catalogFilesDir+"/"+rel)
		if originErr == nil && string(raw) != string(origin) {
			return fmt.Errorf("файл %s изменён пользователем — удалите его вручную или верните исходник", rel)
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			return err
		}
		// пустые родительские каталоги внутри воркспейса убираем
		pruneEmptyDirs(filepath.Dir(target), dir)
	}
	return nil
}

// IsCatalogPackageInstalled — все ли файлы пакета на месте.
func IsCatalogPackageInstalled(dir string, catalogFS fs.FS, pkg CatalogPackage) bool {
	if len(pkg.Files) == 0 {
		return false
	}
	for _, rel := range pkg.Files {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return false
		}
	}
	return true
}

// pruneEmptyDirs — удалять цепочку пустых каталогов вверх до границей root.
func pruneEmptyDirs(dir, root string) {
	for {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			return
		}
		absRoot, err := filepath.Abs(root)
		if err != nil || absDir == absRoot || !strings.HasPrefix(absDir, absRoot+string(filepath.Separator)) {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
