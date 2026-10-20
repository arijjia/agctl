package server

import (
	"net/http"
	"time"
)

const localConnectorGrantTTL = 2 * time.Minute

// localBridgeEnabled gates the experimental pairing API. The transport itself
// (internal/bridge) stays inert until this is explicitly enabled at startup.
func (s *Server) localBridgeEnabled() bool {
	return s.cfg.ExperimentalLocalBridge && s.localBridge != nil
}

// handleLocalBackendGrant issues a one-time pairing bearer for a native
// connector. The response body is intentionally never logged by middleware.
func (s *Server) handleLocalBackendGrant(w http.ResponseWriter, _ *http.Request, id identity) {
	if !s.localBridgeEnabled() {
		writeErr(w, http.StatusNotFound, "локальный connector не включён")
		return
	}
	grant, err := s.localBridge.IssueGrant(id.ID, localConnectorGrantTTL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось создать pairing grant")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"grant":            grant,
		"expiresInSeconds": int(localConnectorGrantTTL.Seconds()),
		"connectPath":      "/api/local-backend/connect",
		"singleUse":        true,
	})
}

func (s *Server) handleLocalBackendStatus(w http.ResponseWriter, _ *http.Request, id identity) {
	if !s.localBridgeEnabled() {
		writeErr(w, http.StatusNotFound, "локальный connector не включён")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": s.localBridge.Connected(id.ID)})
}

func (s *Server) handleLocalBackendRevoke(w http.ResponseWriter, _ *http.Request, id identity) {
	if !s.localBridgeEnabled() {
		writeErr(w, http.StatusNotFound, "локальный connector не включён")
		return
	}
	s.localBridge.Revoke(id.ID)
	w.WriteHeader(http.StatusNoContent)
}
