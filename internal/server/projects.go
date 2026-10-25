package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

func (s *Server) handleProjectsList(w http.ResponseWriter, r *http.Request, id identity) {
	projects, err := provision.ListProjects(s.cfg.Home, id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать проекты")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleProjectCreate(w http.ResponseWriter, r *http.Request, id identity) {
	var req struct {
		Name     string `json:"name"`
		CloneUrl string `json:"cloneUrl"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON проекта")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "ожидался один JSON объект")
		return
	}
	// Клонирование по URL — developer-настройка: это сетевой запрос с сервера,
	// обычному режиму user он не нужен (карточка dev-workspace-clone-url).
	var (
		project provision.Project
		err     error
	)
	if strings.TrimSpace(req.CloneUrl) != "" {
		if id.Role != "developer" && id.Role != "admin" {
			writeErr(w, http.StatusForbidden, "клонирование репозитория по URL доступно роли developer")
			return
		}
		project, err = provision.CloneProject(s.cfg.Home, id.ID, req.Name, req.CloneUrl)
	} else {
		project, err = provision.CreateProject(s.cfg.Home, id.ID, req.Name)
	}
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidProject):
			writeErr(w, http.StatusBadRequest, "недопустимое имя проекта или URL репозитория (только http/https)")
		case errors.Is(err, provision.ErrProjectExists):
			writeErr(w, http.StatusConflict, "проект уже существует")
		default:
			writeErr(w, http.StatusBadGateway, "не удалось клонировать репозиторий: "+err.Error())
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project": project})
}

// autosaveLocalProjects runs for every user's personal volume, including sleeping
// chats. It never provisions a missing volume or adopts non-repository folders.
func (s *Server) autosaveLocalProjects() {
	users, err := s.db.UsersList()
	if err != nil {
		return
	}
	for _, user := range users {
		projects, err := provision.ListProjects(s.cfg.Home, user.ID)
		if err != nil {
			log.Printf("[project-autosave] u%d: cannot list projects", user.ID)
			continue
		}
		for _, project := range projects {
			if _, err := provision.AutosaveLocalProject(s.cfg.Home, user.ID, project.Name); err != nil && !os.IsNotExist(err) {
				// Avoid file paths/content in logs; autosave is invisible to chat UI.
				log.Printf("[project-autosave] u%d: local save failed", user.ID)
			}
		}
	}
}
