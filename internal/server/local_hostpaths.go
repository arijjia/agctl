package server

// local_hostpaths.go — режим «пути как на хосте» (локальный режим, рантайм
// docker): папки проектов видны в контейнере ещё и по своим хостовым путям,
// процесс агента и воркспейс dsh по умолчанию — там же. Пути в сессиях
// совпадают с путями на машине: файл открывается во внешнем редакторе,
// команду агента можно выполнить на хосте. /workspace остаётся (прежние
// сессии и скиллы). Настройка — config.yaml host_paths, переключатель — в
// лаунчере; применяется при перезапуске места.

import (
	"net/http"
	"runtime"

	"github.com/arijjia/agctl/internal/runtime/dockerrt"
)

// LocalHostPathsHooks — config.yaml host_paths (необязательный хук).
type LocalHostPathsHooks interface {
	HostPaths() bool
	SetHostPaths(on bool) error
}

// hostPathsSupported — ОС, где путь хоста можно повторить в Linux-контейнере.
func hostPathsSupported() bool { return runtime.GOOS != "windows" }

// localHostPaths — режим включён и применим (рантайм docker, не Windows).
func (s *Server) localHostPaths() bool {
	h, ok := s.localHooks().(LocalHostPathsHooks)
	if !ok || runtime.GOOS == "windows" {
		return false
	}
	if kind, _ := s.localRuntimes(); kind != "" && kind != "docker" {
		return false
	}
	return h.HostPaths()
}

// hostPathsLayout — где папка хоста видна в контейнере: по своему пути
// (если режим включён и путь монтируется) и всегда — в /workspace.
func (s *Server) hostPathsLayout(host, workspacePath string) string {
	if s.localHostPaths() && dockerrt.HostPathMountable(host) {
		return host
	}
	return workspacePath
}

type hostPathsReq struct {
	Enabled bool `json:"enabled"`
}

// handleLocalHostPathsPut — PUT /api/local/ui/hostpaths {enabled}: включить
// или выключить режим; работающие места показывают «нужен перезапуск».
func (s *Server) handleLocalHostPathsPut(w http.ResponseWriter, r *http.Request, id identity) {
	h, ok := s.localHooks().(LocalHostPathsHooks)
	if !ok {
		writeErr(w, http.StatusNotFound, "режим «пути как на хосте» недоступен")
		return
	}
	if runtime.GOOS == "windows" {
		writeErr(w, http.StatusConflict, "на Windows пути хоста (C:\\…) в Linux-контейнер по тем же путям не смонтировать")
		return
	}
	var req hostPathsReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	if err := h.SetHostPaths(req.Enabled); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	views, err := s.placeViews(id.ID)
	if err == nil {
		for _, v := range views {
			s.markPlacePending(chatKey{uid: id.ID, slot: v.Slot})
		}
	}
	state := "выключен"
	if req.Enabled {
		state = "включён"
	}
	_ = s.db.Audit(id.Login, "local.hostpaths", state, "")
	s.local.events.publish("places")
	writeJSON(w, http.StatusOK, map[string]any{"hostPaths": req.Enabled})
}
