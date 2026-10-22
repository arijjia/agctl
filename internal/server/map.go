package server

// map.go — страница /map: карта системы (archify-диаграммы) за сессией
// agctl. Это первое зерно будущей админ-панели (agctl-hq/docs/1.md);
// сейчас — только список диаграмм и их раздача.

import (
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/arijjia/agctl/system"
)

var mapNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.html$`)

func mapEntries() []string {
	entries, err := fs.ReadDir(system.Map(), ".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && mapNameRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	return names
}

// handleMapIndex — корень /map: список диаграмм (росток админ-панели).
func (s *Server) handleMapIndex(w http.ResponseWriter, r *http.Request, _ identity) {
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>agctl — карта системы</title>`)
	b.WriteString(`<body style="font-family:system-ui;max-width:640px;margin:3rem auto;padding:0 1rem">`)
	b.WriteString(`<h1>Карта системы agctl</h1><p>Будущая админ-панель; сейчас — диаграммы архитектуры.</p><ul>`)
	for _, name := range mapEntries() {
		title := strings.TrimSuffix(name, ".html")
		b.WriteString(`<li><a href="/map/` + name + `">` + html.EscapeString(title) + `</a></li>`)
	}
	b.WriteString(`</ul><p style="opacity:.7">Экспериментальный local backend и K8s-прототип помечены на самой диаграмме.</p></body>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// handleMapFile отдаёт собранную archify-диаграмму; имя строго из белого списка.
func (s *Server) handleMapFile(w http.ResponseWriter, r *http.Request, _ identity) {
	name := r.PathValue("name")
	if !mapNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(system.Map(), name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
