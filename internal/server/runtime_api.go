package server

import "net/http"

// myChatRuntime returns the user's current in-memory runtime, if any.
// Показывается активный слот лаунчера (developer) или основной (user).
func (s *Server) myChatRuntime(userID int64, role string) *chatRuntime {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.chats[chatKey{userID, s.activeChatSlot(userID, role)}]
}

// handleMyRuntimeGet exposes read-only status for the signed-in user. Runtime
// lifecycle remains owned exclusively by agctl serve (request wake + TTL reaper).
func (s *Server) handleMyRuntimeGet(w http.ResponseWriter, _ *http.Request, id identity) {
	if rt := s.myChatRuntime(id.ID, id.Role); rt != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "running", "sshPort": rt.handle.SSHPort, "sshHost": rt.handle.SSHHost})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping", "sshPort": 0})
}
