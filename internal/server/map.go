package server

// map.go — страница /map: карта системы (archify-диаграммы) за сессией
// agctl. Это первое зерно будущей админ-панели (agctl-hq/docs/1.md);
// сейчас — вкладки диаграмм и их раздача.

import (
	"html"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/arijjia/agctl/system"
)

var mapNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.html$`)

// mapEntries — имена HTML-диаграмм во встроенной карте (под регулярку имени).
func mapEntries() []string {
	entries, err := fs.ReadDir(system.Map(), ".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		// index.html — вкладки для просмотра с диска, не диаграмма
		if !e.IsDir() && mapNameRe.MatchString(e.Name()) && e.Name() != "index.html" {
			names = append(names, e.Name())
		}
	}
	return names
}

// mapTabs — порядок и названия вкладок /map; диаграммы вне списка идут
// следом под своими именами файлов.
var mapTabs = []struct{ name, title string }{
	{"agctl-processes", "Сервер — один хост"},
	{"agctl-local", "Ноутбук — локальный режим"},
	{"agctl-cluster", "Кластер: k8s | ax"},
	{"agctl-code", "Бинари и код"},
}

// handleMapIndex — корень /map: вкладки по диаграммам (?m=имя), выбранная
// показывается во фрейме. Без JS: вкладка — обычная ссылка.
func (s *Server) handleMapIndex(w http.ResponseWriter, r *http.Request, _ identity) {
	type tab struct{ name, title string }
	have := map[string]bool{}
	for _, n := range mapEntries() {
		have[strings.TrimSuffix(n, ".html")] = true
	}
	var tabs []tab
	for _, t := range mapTabs {
		if have[t.name] {
			tabs = append(tabs, tab{t.name, t.title})
			delete(have, t.name)
		}
	}
	for _, n := range mapEntries() {
		if n := strings.TrimSuffix(n, ".html"); have[n] {
			tabs = append(tabs, tab{n, n})
		}
	}
	cur := ""
	if len(tabs) > 0 {
		cur = tabs[0].name
	}
	for _, t := range tabs {
		if t.name == r.URL.Query().Get("m") {
			cur = t.name
		}
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>agctl — карта системы</title>`)
	b.WriteString(`<style>html,body{margin:0;height:100%;background:#0b1020;color:#cbd5e1;font-family:system-ui}` +
		`body{display:flex;flex-direction:column}nav{display:flex;gap:4px;align-items:center;padding:8px 14px;border-bottom:1px solid #1e293b}` +
		`nav b{margin-right:12px}nav a{color:#94a3b8;text-decoration:none;padding:6px 12px;border-radius:6px}` +
		`nav a.on{background:#1e293b;color:#f8fafc}nav a:hover{color:#f8fafc}iframe{flex:1;border:0;width:100%}</style>`)
	b.WriteString(`<nav><b>Карта системы agctl</b>`)
	for _, t := range tabs {
		cls := ""
		if t.name == cur {
			cls = ` class="on"`
		}
		b.WriteString(`<a` + cls + ` href="/map?m=` + html.EscapeString(t.name) + `">` + html.EscapeString(t.title) + `</a>`)
	}
	b.WriteString(`</nav>`)
	if cur != "" {
		b.WriteString(`<iframe title="карта" src="/map/` + html.EscapeString(cur) + `.html"></iframe>`)
	}
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
