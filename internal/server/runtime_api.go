package server

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"
)

// requireDeveloper — developer-настройки доступны роли developer и admin.
func requireDeveloper(id identity) bool {
	return id.Role == "developer" || id.Role == "admin"
}

// myChatRuntime возвращает живой персональный рантайм пользователя или nil.
func (s *Server) myChatRuntime(userID int64) *chatRuntime {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.chats[userID]
}

// handleMyRuntimeGet — статус личного рантайма для Кабинета разработчика.
func (s *Server) handleMyRuntimeGet(w http.ResponseWriter, r *http.Request, id identity) {
	if !requireDeveloper(id) {
		writeErr(w, http.StatusForbidden, "управление рантаймом доступно роли developer")
		return
	}
	if rt := s.myChatRuntime(id.ID); rt != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "running", "sshPort": rt.handle.SSHPort})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping", "sshPort": 0})
}

// handleMyRuntimeStart — ленивый ensure: том сохраняется, compute создаётся.
func (s *Server) handleMyRuntimeStart(w http.ResponseWriter, r *http.Request, id identity) {
	if !requireDeveloper(id) {
		writeErr(w, http.StatusForbidden, "управление рантаймом доступно роли developer")
		return
	}
	if _, err := s.ensureChat(id.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось запустить рантайм: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "runtime.start", strconv.FormatInt(id.ID, 10), "developer=1")
	writeJSON(w, http.StatusOK, map[string]any{"status": "running"})
}

// handleMyRuntimeStop — останавливает compute; том и сессии остаются на диске.
func (s *Server) handleMyRuntimeStop(w http.ResponseWriter, r *http.Request, id identity) {
	if !requireDeveloper(id) {
		writeErr(w, http.StatusForbidden, "управление рантаймом доступно роли developer")
		return
	}
	rt := s.myChatRuntime(id.ID)
	if rt == nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
		return
	}
	s.chatMu.Lock()
	delete(s.chats, id.ID)
	s.chatMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s.forgetToken(chatTokenCacheKey(rt.handle))
	if err := s.chatProvider.StopChat(ctx, rt.handle); err != nil {
		log.Printf("[chat] developer %s: stop: %v", id.Login, err)
		writeErr(w, http.StatusInternalServerError, "не удалось остановить рантайм: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "runtime.stop", strconv.FormatInt(id.ID, 10), "developer=1")
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping"})
}

const myRuntimeLogsMaxTail = 5000

// handleMyRuntimeLogs — хвост логов контейнера личного рантайма.
// Boot-токен dsh виден в логах Docker в любом случае; отдаём их только
// developer/admin своего же рантайма.
func (s *Server) handleMyRuntimeLogs(w http.ResponseWriter, r *http.Request, id identity) {
	if !requireDeveloper(id) {
		writeErr(w, http.StatusForbidden, "логи рантайма доступны роли developer")
		return
	}
	rt := s.myChatRuntime(id.ID)
	if rt == nil {
		writeErr(w, http.StatusConflict, "рантайм спит: нажмите «Запустить» и повторите")
		return
	}
	tail := r.URL.Query().Get("tail")
	if n, err := strconv.Atoi(tail); err != nil || n <= 0 || n > myRuntimeLogsMaxTail {
		tail = "300"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	logs, err := s.chatProvider.LogsChat(ctx, rt.handle, tail)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать логи: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(logs))
}
