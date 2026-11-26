package server

// local_runtime.go — выбор рантайма рабочего места в лаунчере локального
// режима: docker (контейнер) или sbx (Docker Sandboxes, microVM). Выбор
// хранит хост (dev.yaml, LocalRuntimeHooks); действует со следующего
// (пере)запуска рабочего места — работающие помечаются «нужен перезапуск».

import (
	"net/http"
	"strings"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/sbxrt"
)

// LocalRuntime — рантайм для выбора в лаунчере.
type LocalRuntime struct {
	ID        string `json:"id"`               // docker | sbx
	Title     string `json:"title"`            // подпись для интерфейса
	Available bool   `json:"available"`        // можно ли выбрать на этой машине
	Detail    string `json:"detail,omitempty"` // версия либо причина недоступности
}

// LocalRuntimeHooks — необязательная часть LocalHooks: выбор рантайма.
// Хост без неё — только Docker.
type LocalRuntimeHooks interface {
	RuntimeKind() string
	Runtimes() []LocalRuntime
	SetRuntimeKind(kind string) error
}

// localRuntimes — текущий рантайм и список для лаунчера.
func (s *Server) localRuntimes() (string, []LocalRuntime) {
	if h, ok := s.localHooks().(LocalRuntimeHooks); ok {
		return h.RuntimeKind(), h.Runtimes()
	}
	return sbxrt.KindDocker, []LocalRuntime{{ID: sbxrt.KindDocker, Title: "Docker", Available: true}}
}

// handleKind — рантайм, в котором поднято рабочее место.
func handleKind(h agctlrt.ChatHandle) string {
	if strings.HasPrefix(h.InstanceID, sbxrt.InstancePrefix) {
		return sbxrt.KindSbx
	}
	return sbxrt.KindDocker
}

type localRuntimeReq struct {
	Runtime string `json:"runtime"`
}

// handleLocalRuntimePut — PUT /api/local/ui/runtime {"runtime":"docker|sbx"}.
func (s *Server) handleLocalRuntimePut(w http.ResponseWriter, r *http.Request, id identity) {
	h, ok := s.localHooks().(LocalRuntimeHooks)
	if !ok {
		writeErr(w, http.StatusNotFound, "выбор рантайма недоступен")
		return
	}
	var req localRuntimeReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	kind := strings.TrimSpace(req.Runtime)
	var found *LocalRuntime
	for _, rt := range h.Runtimes() {
		if rt.ID == kind {
			found = &rt
			break
		}
	}
	if found == nil {
		writeErr(w, http.StatusBadRequest, "неизвестный рантайм (docker | sbx)")
		return
	}
	if !found.Available {
		writeErr(w, http.StatusConflict, "рантайм "+kind+" недоступен на этой машине: "+found.Detail)
		return
	}
	if err := h.SetRuntimeKind(kind); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Рантайм общий для всех рабочих мест: работающие в другом — на старом.
	restart := false
	s.chatMu.Lock()
	var stale []int
	for k, rt := range s.chats {
		if k.uid == s.local.ownerID && handleKind(rt.handle) != kind {
			stale = append(stale, k.slot)
		}
	}
	s.chatMu.Unlock()
	for _, slot := range stale {
		s.placeTask(slot).markPending()
		restart = true
	}
	s.local.bumpMain()
	s.local.events.publish("places")
	_ = s.db.Audit(id.Login, "local.runtime.set", kind, "")
	writeJSON(w, http.StatusOK, map[string]any{"runtime": kind, "restartNeeded": restart})
}
