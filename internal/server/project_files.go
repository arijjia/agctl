package server

// project_files.go — файлы воркспейса для обычного пользователя: список и
// скачивание через gateway. Зачем: превью артефактов агента (например,
// готовой презентации) живёт в правой панели dsh, а скачивание должно
// работать всегда и без dsh-Internal'ов — платформа отдаёт файл из личного
// тома напрямую (только СВОЙ воркспейс, только чтение).

import (
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Ограничения выдачи файлов: список — неглубокий и небольшой, скачивание —
// с потолком размера (артефакты агента: pptx/доки/картинки, не тома).
const (
	maxFileListEntries = 500
	maxFileListDepth   = 6
	maxFileDownload    = 200 << 20 // 200 МиБ
)

// skipDirName — служебные каталоги, которые в списке файлов не показываем:
// git-история и stash платформы не артефакты пользователя.
func skipDirName(name string) bool {
	switch name {
	case ".git", ".agctl", "node_modules", ".venv", "venv", "__pycache__", ".pytest_cache":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// projectFileEntry — одна строка списка файлов.
type projectFileEntry struct {
	Path  string `json:"path"` // путь относительно корня воркспейса (/)
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

// listProjectFiles — обход рабочего дерева воркспейса с границами глубины
// и количества. Ошибки отдельных записей игнорируются (список — best effort).
func listProjectFiles(dir string) []projectFileEntry {
	out := []projectFileEntry{}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(out) >= maxFileListEntries {
			return fs.SkipAll
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil || rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDirName(d.Name()) || strings.Count(rel, string(os.PathSeparator)) >= maxFileListDepth {
				return fs.SkipDir
			}
			out = append(out, projectFileEntry{Path: filepath.ToSlash(rel) + "/", Size: 0, IsDir: true})
			return nil
		}
		if skipDirName(d.Name()) || strings.Count(rel, string(os.PathSeparator)) > maxFileListDepth {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		out = append(out, projectFileEntry{Path: filepath.ToSlash(rel), Size: info.Size(), IsDir: false})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return false
		}
		// каталоги вперёд, затем по пути
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Path < out[j].Path
	})
	if len(out) > maxFileListEntries {
		out = out[:maxFileListEntries]
	}
	return out
}

// handleProjectFilesList — GET /api/projects/{name}/files: список файлов
// воркспейса (без .git/.agctl/node_modules и прочих служебных каталогов).
func (s *Server) handleProjectFilesList(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": listProjectFiles(dir)})
}

// handleProjectFileDownload — GET /api/projects/{name}/file?path=<отн. путь>:
// отдать файл воркспейса как вложение (Content-Disposition: attachment).
// Границы: путь строго внутри каталога проекта (Rel-гард), только regular
// файлы без симлинков, размер ≤ maxFileDownload.
func (s *Server) handleProjectFileDownload(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	rel := r.URL.Query().Get("path")
	clean := filepath.Clean("/" + strings.TrimPrefix(filepath.ToSlash(rel), "/")) // убить ../
	if clean == "/" || strings.Contains(clean, "..") {
		writeErr(w, http.StatusBadRequest, "некорректный путь файла")
		return
	}
	rel = strings.TrimPrefix(clean, "/")
	target := filepath.Join(dir, filepath.FromSlash(rel))
	inside, err := filepath.Rel(dir, target)
	if err != nil || inside == "." || strings.HasPrefix(inside, "..") {
		writeErr(w, http.StatusBadRequest, "путь вне воркспейса")
		return
	}
	info, err := os.Lstat(target) // Lstat: симлинк не разыменовывается — скачать через ссылку файл вне тома нельзя (дефект D9)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, http.StatusNotFound, "файл не найден")
		} else {
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if !info.Mode().IsRegular() {
		writeErr(w, http.StatusBadRequest, "это не регулярный файл (симлинки и спецфайлы не скачиваются)")
		return
	}
	if info.Size() > maxFileDownload {
		writeErr(w, http.StatusBadRequest, "файл больше 200 МиБ — скачайте его из терминала developer-доступом")
		return
	}
	f, err := os.Open(target) // #nosec G304 — путь прошёл Rel-гард внутри dir
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	name := filepath.Base(target)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename*=UTF-8''`+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, name, info.ModTime(), f)
	_ = s.db.Audit(id.Login, "project.file.download", r.PathValue("name"), rel)
}
