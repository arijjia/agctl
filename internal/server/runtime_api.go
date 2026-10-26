package server

import "net/http"

// myChatRuntime returns the user's current in-memory runtime, if any.
func (s *Server) myChatRuntime(userID int64) *chatRuntime {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.chats[userID]
}

// handleMyRuntimeGet exposes read-only status for the signed-in user. Runtime
// lifecycle remains owned exclusively by agctl serve (request wake + TTL reaper).
func (s *Server) handleMyRuntimeGet(w http.ResponseWriter, _ *http.Request, id identity) {
	if rt := s.myChatRuntime(id.ID); rt != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "running", "sshPort": rt.handle.SSHPort})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "sleeping", "sshPort": 0})
}
