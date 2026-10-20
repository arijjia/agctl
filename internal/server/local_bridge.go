package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/bridge"
)

const (
	localConnectorGrantTTL = 2 * time.Minute
	// connectorTokenPrefix makes a durable credential self-describing on the
	// wire so the server can pick the right lookup without ambiguity.
	connectorTokenPrefix = "agctl-conn-1-"
)

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

// handleLocalBackendConnect authenticates the connector bearer (one-time grant
// for pairing or the durable connector credential for reconnects), issues the
// durable credential on first pairing and hands the upgrade to the bridge.
func (s *Server) handleLocalBackendConnect(w http.ResponseWriter, r *http.Request) {
	if !s.localBridgeEnabled() {
		http.NotFound(w, r)
		return
	}
	userID, connectorToken, err := s.resolveLocalConnectorAuth(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "connector не авторизован")
		return
	}
	s.localBridge.AcceptConnector(userID, w, r, connectorToken)
}

// resolveLocalConnectorAuth distinguishes a one-time pairing grant from a
// durable connector credential by prefix and returns (userID, newCredential).
func (s *Server) resolveLocalConnectorAuth(r *http.Request) (int64, string, error) {
	token := bridge.BearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return 0, "", bridge.ErrInvalidGrant
	}
	if strings.HasPrefix(token, connectorTokenPrefix) {
		hash := sha256.Sum256([]byte(token))
		connector, err := s.db.LocalConnectorByHash(base64.RawStdEncoding.EncodeToString(hash[:]))
		if err != nil {
			return 0, "", bridge.ErrInvalidGrant
		}
		_ = s.db.LocalConnectorTouch(connector.ID)
		return connector.UserID, "", nil
	}
	userID, err := s.localBridge.ConsumeGrant(token)
	if err != nil {
		return 0, "", err
	}
	// Pairing succeeded: issue the durable credential (raw value is delivered
	// to the connector over the control stream; only the hash is stored).
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return 0, "", err
	}
	connectorToken := connectorTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(connectorToken))
	if _, err := s.db.LocalConnectorCreate(userID, base64.RawStdEncoding.EncodeToString(hash[:]), "local"); err != nil {
		return 0, "", err
	}
	return userID, connectorToken, nil
}

func (s *Server) handleLocalBackendStatus(w http.ResponseWriter, _ *http.Request, id identity) {
	if !s.localBridgeEnabled() {
		writeErr(w, http.StatusNotFound, "локальный connector не включён")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connected": s.localBridge.Connected(id.ID)})
}

// handleLocalBackendRevoke closes the live session and deletes the durable
// credential: reconnecting afterwards requires a fresh pairing grant.
func (s *Server) handleLocalBackendRevoke(w http.ResponseWriter, _ *http.Request, id identity) {
	if !s.localBridgeEnabled() {
		writeErr(w, http.StatusNotFound, "локальный connector не включён")
		return
	}
	s.localBridge.Revoke(id.ID)
	if err := s.db.LocalConnectorsDeleteForUser(id.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось отозвать credential")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
