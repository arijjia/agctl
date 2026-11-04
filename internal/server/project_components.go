package server

// project_components.go — консоль компонентов воркспейса (сценарий 1c-vibe):
// каталог скиллов/инструментов/промптов проекта и серверное включение-выключение
// после клонирования. Истина о состоянии — рабочее дерево проекта (stash);
// KV-настройка хранит выбор пользователя для аудита и будущих реаплая.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

const maxComponentsBodyBytes = 64 << 10 // список ID не может быть большим

// componentsSettingKey — ключ KV с последним применённым выбором.
func componentsSettingKey(userID int64, project string) string {
	return "project-components-u" + strconv.FormatInt(userID, 10) + "-" + project
}

// resolveProjectDir — безопасное разрешение каталога проекта для API-обработчиков.
func (s *Server) resolveProjectDir(userID int64, name string) (string, int, string) {
	if !provision.ValidProjectName(name) {
		return "", http.StatusBadRequest, "некорректное имя проекта"
	}
	dir, err := provision.ProjectDirectory(s.cfg.Home, userID, name)
	if err != nil {
		if errors.Is(err, provision.ErrInvalidProject) {
			return "", http.StatusBadRequest, "некорректное имя проекта"
		}
		return "", http.StatusNotFound, "воркспейс не найден"
	}
	return dir, 0, ""
}

// writeComponentsCatalog — единая форма ответа GET и PUT.
func writeComponentsCatalog(w http.ResponseWriter, dir string) {
	components := provision.ScanProjectComponents(dir)
	if components == nil {
		components = []provision.ProjectComponent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"components": components})
}

// requireDeveloper — компоненты воркспейса настраивает роль developer/admin
// (разделение по ролям шире — отдельная задача платформы).
func (s *Server) requireDeveloper(next func(http.ResponseWriter, *http.Request, identity)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, id identity) {
		if id.Role != "developer" && id.Role != "admin" {
			writeErr(w, http.StatusForbidden, "настройка компонентов воркспейса доступна роли developer")
			return
		}
		next(w, r, id)
	})
}

// handleProjectComponentsGet — GET /api/projects/{name}/components.
func (s *Server) handleProjectComponentsGet(w http.ResponseWriter, r *http.Request, id identity) {
	dir, code, msg := s.resolveProjectDir(id.ID, r.PathValue("name"))
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	writeComponentsCatalog(w, dir)
}

// handleProjectComponentsPut — PUT /api/projects/{name}/components с телом
// {"disabled":[...]}. Приводит рабочее дерево к выбору и запоминает его.
func (s *Server) handleProjectComponentsPut(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("name")
	dir, code, msg := s.resolveProjectDir(id.ID, name)
	if code != 0 {
		writeErr(w, code, msg)
		return
	}
	var req struct {
		Disabled []string `json:"disabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxComponentsBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON выбора компонентов")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "ожидался один JSON объект")
		return
	}
	components := provision.ScanProjectComponents(dir)
	if err := provision.ValidateComponentSelection(components, req.Disabled); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := provision.ApplyProjectComponents(dir, req.Disabled); err != nil {
		log.Printf("[components] u%d %s: apply failed", id.ID, name)
		writeErr(w, http.StatusInternalServerError, "не удалось применить выбор компонентов")
		return
	}
	state := provision.ProjectComponentsState{Schema: 1, Disabled: normalizeDisabledIDs(req.Disabled)}
	body, encErr := provision.EncodeComponentsState(state)
	if encErr == nil {
		if err := s.db.SettingSet(componentsSettingKey(id.ID, name), body); err != nil {
			log.Printf("[components] u%d %s: state not persisted", id.ID, name)
		}
	}
	changed := len(state.Disabled)
	_ = s.db.Audit(id.Login, "project.components.update", name, fmt.Sprintf("disabled=%d", changed))
	writeComponentsCatalog(w, dir)
}

// normalizeDisabledIDs — обрезка пробелов, отброс пустых и дублей.
func normalizeDisabledIDs(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
