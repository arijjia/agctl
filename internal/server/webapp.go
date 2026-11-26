package server

// webapp.go — общие страницы единого веб-приложения (оба режима):
//
//	«Профиль»  — исходник profile.yaml организации: чтение, проверка, запись
//	             в каталог профиля на диске (тот же файл, что лежит в репо
//	             профиля) и замена логотипа;
//	«Скачать»  — как поставить agctl разработчику: команды установки и
//	             файлы, которые организация раздаёт со своего сервера;
//	«Где что лежит» (локальный режим) — отчёт agctl dev paths.
//
// Править профиль может администратор сервера или владелец локального режима.

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/profile"
)

// LocalPathsHooks — необязательная часть хоста лаунчера: отчёт о путях,
// портах и контейнерах дома (то же, что печатает agctl dev paths).
type LocalPathsHooks interface {
	PathsReport() any
}

func (s *Server) registerWebAppRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/profile/source", s.requireManager(s.handleProfileSourceGet))
	mux.HandleFunc("PUT /api/profile/source", s.requireManager(s.handleProfileSourcePut))
	mux.HandleFunc("POST /api/profile/source/check", s.requireManager(s.handleProfileSourceCheck))
	mux.HandleFunc("PUT /api/profile/logo", s.requireManager(s.handleProfileLogoPut))
	mux.HandleFunc("GET /api/downloads", s.requireUser(s.handleDownloads))
	mux.HandleFunc("GET /downloads/{file}", s.requireUser(s.handleDownloadFile))
	mux.HandleFunc("GET /api/local/ui/paths", s.requireUser(s.handleLocalPaths))
}

// requireManager — администратор сервера или владелец локального режима.
func (s *Server) requireManager(next func(http.ResponseWriter, *http.Request, identity)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, id identity) {
		owner := s.isLocal() && s.local != nil && id.ID == s.local.ownerID
		if id.Role != "admin" && !owner {
			writeErr(w, http.StatusForbidden, "нужна роль администратора")
			return
		}
		if r.Method != http.MethodGet {
			if o := r.Header.Get("Origin"); o != "" && !sameOriginAuthority(o, r.Host) {
				writeErr(w, http.StatusForbidden, "чужой Origin")
				return
			}
		}
		next(w, r, id)
	})
}

// profileDir — каталог профиля на диске; "" — профиль встроен в бинарь.
func (s *Server) profileDir() string { return s.cfg.Profile.SourceDir() }

func (s *Server) profileID() string {
	if s.cfg.Profile != nil {
		return s.cfg.Profile.ID
	}
	return "agctl"
}

func (s *Server) handleProfileSourceGet(w http.ResponseWriter, r *http.Request, id identity) {
	dir := s.profileDir()
	out := map[string]any{"id": s.profileID(), "dir": dir, "writable": false, "yaml": "", "files": []string{}}
	if dir == "" {
		out["reason"] = "Профиль встроен в agctl и не редактируется. Скопируйте profiles/example, задайте свой id и подключите каталог: agctl dev init --profile-dir <путь>."
		writeJSON(w, http.StatusOK, out)
		return
	}
	raw, err := os.ReadFile(filepath.Join(dir, "profile.yaml")) // #nosec G304 — каталог профиля из настроек запуска
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "profile.yaml: "+err.Error())
		return
	}
	out["yaml"] = string(raw)
	out["writable"] = true
	if f, err := os.OpenFile(filepath.Join(dir, "profile.yaml"), os.O_WRONLY, 0); err != nil { // #nosec G304
		out["writable"] = false
		out["reason"] = "Каталог профиля доступен только для чтения: " + err.Error()
	} else {
		_ = f.Close()
	}
	out["files"] = profileTree(dir)
	if real, err := filepath.EvalSymlinks(dir); err == nil && real != dir {
		out["realDir"] = real
	}
	writeJSON(w, http.StatusOK, out)
}

// profileTree — файлы профиля для показа «что лежит в каталоге» (до 3
// уровней, без служебных каталогов).
func profileTree(dir string) []string {
	out := []string{}
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if rel == "." {
			return nil
		}
		name := d.Name()
		if d.IsDir() && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "out") {
			return filepath.SkipDir
		}
		depth := strings.Count(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			if depth >= 2 {
				out = append(out, filepath.ToSlash(rel)+"/…")
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".bak") {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(out)
	if len(out) > 200 {
		out = append(out[:200], "…")
	}
	return out
}

func (s *Server) readProfileBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	dir := s.profileDir()
	if dir == "" {
		writeErr(w, http.StatusConflict, "профиль встроен в agctl и не редактируется")
		return nil, "", false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 256<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл больше 256 КБ")
		return nil, "", false
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		writeErr(w, http.StatusBadRequest, "пустой profile.yaml")
		return nil, "", false
	}
	return raw, dir, true
}

// handleProfileSourceCheck — проверить текст без записи (кнопка «Проверить»
// и проверка перед сохранением формы).
func (s *Server) handleProfileSourceCheck(w http.ResponseWriter, r *http.Request, id identity) {
	raw, dir, ok := s.readProfileBody(w, r)
	if !ok {
		return
	}
	if _, err := profile.CheckSource(dir, s.profileID(), raw); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleProfileSourcePut(w http.ResponseWriter, r *http.Request, id identity) {
	raw, dir, ok := s.readProfileBody(w, r)
	if !ok {
		return
	}
	if _, err := profile.CheckSource(dir, s.profileID(), raw); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := profile.WriteSource(dir, raw); err != nil {
		writeErr(w, http.StatusInternalServerError, "запись profile.yaml: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "profile.source.save", s.profileID(), "")
	// Профиль читается при старте: бренд, модели и образы обновятся после
	// перезапуска платформы.
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "restartNeeded": true,
		"path": filepath.Join(dir, "profile.yaml")})
}

// handleProfileLogoPut — заменить логотип: тело запроса — PNG. Файл пишется
// туда, куда указывает brand.logo (по умолчанию assets/logo.png).
func (s *Server) handleProfileLogoPut(w http.ResponseWriter, r *http.Request, id identity) {
	dir := s.profileDir()
	if dir == "" {
		writeErr(w, http.StatusConflict, "профиль встроен в agctl и не редактируется")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, profile.MaxLogoBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "логотип больше 512 КБ")
		return
	}
	if !bytes.HasPrefix(raw, []byte("\x89PNG\r\n\x1a\n")) {
		writeErr(w, http.StatusBadRequest, "нужен файл PNG")
		return
	}
	rel := "assets/logo.png"
	if s.cfg.Profile != nil && s.cfg.Profile.Brand.Logo != "" {
		rel = s.cfg.Profile.Brand.Logo
	}
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.WriteFile(target, raw, 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "profile.logo.save", s.profileID(), "")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "path": rel, "restartNeeded": true})
}

// ── «Скачать» ───────────────────────────────────────────────────────────────

// downloadsDir — каталог файлов, которые организация раздаёт со своего
// сервера (комплекты agctl для контура без доступа к GitHub): <дом>/downloads.
func (s *Server) downloadsDir() string { return filepath.Join(s.cfg.Home, "downloads") }

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request, id identity) {
	type file struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"url"`
	}
	files := []file{}
	if entries, err := os.ReadDir(s.downloadsDir()); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if info, err := e.Info(); err == nil {
				files = append(files, file{Name: e.Name(), Size: info.Size(), URL: "/downloads/" + e.Name()})
			}
		}
	}
	kitRepo := ""
	if s.cfg.Profile != nil {
		kitRepo = strings.TrimSpace(s.cfg.Profile.Install.Repo)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":      s.cfg.Version,
		"profile":      s.profileID(),
		"coreRepo":     "arijjia/agctl",
		"kitRepo":      kitRepo,
		"files":        files,
		"downloadsDir": s.downloadsDir(),
		"local":        s.isLocal(),
	})
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("file")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.downloadsDir(), name)
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	http.ServeFile(w, r, path)
}

// ── «Где что лежит» ─────────────────────────────────────────────────────────

func (s *Server) handleLocalPaths(w http.ResponseWriter, r *http.Request, id identity) {
	h, ok := s.localHooks().(LocalPathsHooks)
	if !ok || s.local == nil || id.ID != s.local.ownerID {
		writeErr(w, http.StatusNotFound, "доступно только в локальном режиме (agctl dev)")
		return
	}
	writeJSON(w, http.StatusOK, h.PathsReport())
}
