package provision

// dsh_skills.go — скиллы контейнера из пакетов каталога профиля: файлы
// пакета с путями .dsh/skills/<имя>/… кладутся в <dshHome>/skills/<имя>/…
// (пользовательский корень скиллов dsh — общий для всех сессий контейнера).
// Скиллы не секретны: каталоги 0755, файлы 0644, как у скилла моста.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dshSkillsFromPrefix — префикс путей внутри пакета, который переносится в
// корень скиллов dsh-home; файлы вне его на уровне контейнера не ставятся.
const dshSkillsFromPrefix = ".dsh/skills/"

// skillTargetRel — относительный путь файла пакета внутри <dshHome>/skills;
// ok=false — файл пакета не скилл и на уровень контейнера не переносится.
func skillTargetRel(rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, dshSkillsFromPrefix) || len(rel) <= len(dshSkillsFromPrefix) {
		return "", false
	}
	return strings.TrimPrefix(rel, dshSkillsFromPrefix), true
}

// SkillPackageFiles — пути файлов скилла пакета внутри <dshHome>/skills.
func SkillPackageFiles(pkg CatalogPackage) []string {
	out := []string{}
	for _, rel := range pkg.Files {
		if t, ok := skillTargetRel(rel); ok {
			out = append(out, t)
		}
	}
	return out
}

// InstallSkillPackageToDshHome копирует скиллы пакета в <dshHome>/skills.
// Существующие файлы перезаписываются (переустановка = обновление до версии
// каталога). Идемпотентно.
func InstallSkillPackageToDshHome(dshHome string, catalogFS fs.FS, pkg CatalogPackage) error {
	skillsRoot := filepath.Join(dshHome, "skills")
	for _, rel := range pkg.Files {
		target, ok := skillTargetRel(rel)
		if !ok {
			continue
		}
		data, err := fs.ReadFile(catalogFS, catalogPrefix+"/"+pkg.ID+"/"+catalogFilesDir+"/"+rel)
		if err != nil {
			return fmt.Errorf("файл %s: %w", rel, err)
		}
		full := filepath.Join(skillsRoot, filepath.FromSlash(target))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// RemoveSkillPackageFromDshHome удаляет скиллы пакета из <dshHome>/skills.
// Изменённый пользователем файл не удаляется молча — возврат ошибки, чтобы
// не потерять работу. Отсутствующие файлы пропускаются (идемпотентно).
func RemoveSkillPackageFromDshHome(dshHome string, catalogFS fs.FS, pkg CatalogPackage) error {
	skillsRoot := filepath.Join(dshHome, "skills")
	for _, rel := range pkg.Files {
		target, ok := skillTargetRel(rel)
		if !ok {
			continue
		}
		full := filepath.Join(skillsRoot, filepath.FromSlash(target))
		raw, err := os.ReadFile(full) // #nosec G304 — путь собран из валидированных частей
		if err != nil {
			if os.IsNotExist(err) {
				continue // уже нет — идемпотентно
			}
			return err
		}
		origin, originErr := fs.ReadFile(catalogFS, catalogPrefix+"/"+pkg.ID+"/"+catalogFilesDir+"/"+rel)
		if originErr == nil && string(raw) != string(origin) {
			return fmt.Errorf("скилл-файл %s изменён пользователем — удалите его вручную или верните исходник", target)
		}
		if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
			return err
		}
		pruneEmptyDirs(filepath.Dir(full), skillsRoot)
	}
	return nil
}
