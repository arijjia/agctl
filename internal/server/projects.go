package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

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
		Name string `json:"name"`
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
	project, err := provision.CreateProject(s.cfg.Home, id.ID, req.Name)
	if err != nil {
		switch {
		case errors.Is(err, provision.ErrInvalidProject):
			writeErr(w, http.StatusBadRequest, "недопустимое имя или путь проекта")
		case errors.Is(err, provision.ErrProjectExists):
			writeErr(w, http.StatusConflict, "проект уже существует")
		default:
			writeErr(w, http.StatusInternalServerError, "не удалось создать проект")
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
