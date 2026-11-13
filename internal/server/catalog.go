package server

// catalog.go — «возможности воркспейса»: платформенный каталог пакетов
// (скиллы/шаблоны/инструменты), установка и удаление в конкретный воркспейс,
// и системный промт воркспейса (AGENTS.md) — один на воркспейс.
//
// Это единое место правды о настройках воркспейса: консоль воркспейсов
// (dsh-плагин) и диалог создания (chat-brand) работают с одними и теми же
// API. Доступ — requireUser: каждый настраивает только свои воркспейсы
// (resolveProjectDir ограничивает путь личным томом пользователя).

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

// maxPromptBytes — верхняя граница AGENTS.md воркспейса при записи.
const maxPromptBytes = 256 << 10

func (s *Server) catalogPackages() []provision.CatalogPackage {
	pkgs, err := provision.CatalogPackages(s.brand.FS)
	if err != nil {
		return []provision.CatalogPackage{}
	}
	return pkgs
}

// handleCatalogList — GET /api/catalog: доступные платформенные пакеты
// (без привязки к воркспейсу — для диалога создания).
func (s *Server) handleCatalogList(w http.ResponseWriter, r *http.Request, _ identity) {
	type entry struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	out := []entry{}
	for _, p := range s.catalogPackages() {
		out = append(out, entry{ID: p.ID, Title: p.Title, Description: p.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": out})
}

// handleProjectCatalogGet — GET /api/projects/{name}/catalog: пакеты со
// флагом installed для этого воркспейса (консоль воркспейсов).
func (s *Server) handleProjectCatalogGet(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	type entry struct {
		provision.CatalogPackage
		Installed bool `json:"installed"`
	}
	out := []entry{}
	for _, p := range s.catalogPackages() {
		out = append(out, entry{CatalogPackage: p, Installed: provision.IsCatalogPackageInstalled(dir, s.brand.FS, p)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": out})
}

// handleProjectCatalogInstall — POST /api/projects/{name}/catalog/{id}/install:
// поставить пакет в воркспейс (файлы 1:1; переустановка = обновление).
func (s *Server) handleProjectCatalogInstall(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	pkg, err := provision.FindCatalogPackage(s.brand.FS, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err := provision.InstallCatalogPackage(dir, s.brand.FS, pkg); err != nil {
		writeErr(w, http.StatusInternalServerError, "установка пакета: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "project.catalog.install", r.PathValue("name"), pkg.ID)
	writeJSON(w, http.StatusOK, map[string]any{"installed": true, "package": pkg.ID})
}

// handleProjectCatalogDelete — DELETE /api/projects/{name}/catalog/{id}:
// убрать пакет из воркспейса (изменённые пользователем файлы не трогаем).
func (s *Server) handleProjectCatalogDelete(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	pkg, err := provision.FindCatalogPackage(s.brand.FS, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if err := provision.RemoveCatalogPackage(dir, s.brand.FS, pkg); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "project.catalog.remove", r.PathValue("name"), pkg.ID)
	writeJSON(w, http.StatusOK, map[string]any{"installed": false, "package": pkg.ID})
}

// handleProjectPromptGet — GET /api/projects/{name}/prompt: текст AGENTS.md
// воркспейса (один системный промт на воркспейс; пусто = файла нет).
func (s *Server) handleProjectPromptGet(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	raw, err := os.ReadFile(promptPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, map[string]any{"prompt": ""})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompt": string(raw)})
}

// promptPutReq — тело PUT /api/projects/{name}/prompt.
type promptPutReq struct {
	Prompt string `json:"prompt"`
}

// handleProjectPromptPut — PUT /api/projects/{name}/prompt: записать
// AGENTS.md воркспейса. Пустой промт удаляет файл (воркспейс без промта).
func (s *Server) handleProjectPromptPut(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	var req promptPutReq
	// строгий декод: неизвестные поля — ошибка (случайный {} не должен
	// стирать промт воркспейса)
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPromptBytes+1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON промта")
		return
	}
	if len(req.Prompt) > maxPromptBytes {
		writeErr(w, http.StatusBadRequest, "промт больше 256 КиБ")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		if err := os.Remove(promptPath(dir)); err != nil && !os.IsNotExist(err) {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := os.WriteFile(promptPath(dir), []byte(req.Prompt), 0o600); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "project.prompt.set", r.PathValue("name"), "")
	writeJSON(w, http.StatusOK, map[string]any{"saved": true})
}

// promptPath — путь системного промта воркспейса.
func promptPath(dir string) string {
	return dir + string(os.PathSeparator) + "AGENTS.md"
}
