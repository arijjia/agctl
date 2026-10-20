package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type localGrantResponse struct {
	Grant            string `json:"grant"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
	ConnectPath      string `json:"connectPath"`
	SingleUse        bool   `json:"singleUse"`
}

func TestLocalBridgeRoutesDisabledByDefault(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	if code, _ := e.do(e.admin, "POST", "/api/local-backend/grant", nil); code != http.StatusNotFound {
		t.Fatalf("grant without experimental flag = %d, want 404", code)
	}
	if code, _ := e.do(e.admin, "GET", "/api/local-backend/status", nil); code != http.StatusNotFound {
		t.Fatalf("status without experimental flag = %d, want 404", code)
	}
	if code, _ := e.do(e.admin, "DELETE", "/api/local-backend", nil); code != http.StatusNotFound {
		t.Fatalf("revoke without experimental flag = %d, want 404", code)
	}
}

func TestLocalBridgePairingRequiresAuth(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.ExperimentalLocalBridge = true })
	if code, _ := e.do(e.newClient(), "POST", "/api/local-backend/grant", nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous grant = %d, want 401", code)
	}
}

func TestLocalBridgeGrantStatusRevokeLifecycle(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.ExperimentalLocalBridge = true })
	e.register(e.admin, "anna", "secret1")

	code, body := e.do(e.admin, "POST", "/api/local-backend/grant", nil)
	if code != http.StatusCreated {
		t.Fatalf("grant = %d %s, want 201", code, body)
	}
	var granted localGrantResponse
	if err := json.Unmarshal([]byte(body), &granted); err != nil {
		t.Fatal(err)
	}
	if granted.Grant == "" || !granted.SingleUse || granted.ExpiresInSeconds <= 0 || granted.ConnectPath != "/api/local-backend/connect" {
		t.Fatalf("unexpected grant payload: %+v", granted)
	}
	if strings.Contains(body, granted.Grant) && len(granted.Grant) < 40 {
		t.Fatal("grant token looks too short to be a 32-byte secret")
	}

	// Connector handshake consumes the grant (Authorization bearer only).
	wsURL := "ws" + strings.TrimPrefix(e.api.URL, "http") + granted.ConnectPath
	ws, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + granted.Grant}},
	})
	if err != nil {
		t.Fatalf("connector dial with grant: %v", err)
	}
	defer ws.CloseNow()

	// Replay with the same one-time grant must fail.
	if _, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + granted.Grant}},
	}); err == nil {
		t.Fatal("grant replay unexpectedly succeeded")
	}

	// Status flips to connected for this user's session.
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body = e.do(e.admin, "GET", "/api/local-backend/status", nil)
		if code != http.StatusOK {
			t.Fatalf("status = %d %s, want 200", code, body)
		}
		var status struct {
			Connected bool `json:"connected"`
		}
		if err := json.Unmarshal([]byte(body), &status); err != nil {
			t.Fatal(err)
		}
		if status.Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connector never became connected; last body=%s", body)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Revoke closes the connector session and invalidates further pairing.
	if code, _ := e.do(e.admin, "DELETE", "/api/local-backend", nil); code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := ws.Read(ctx); err == nil {
		t.Fatal("connector session survived revoke")
	}
	code, body = e.do(e.admin, "GET", "/api/local-backend/status", nil)
	if code != http.StatusOK || !strings.Contains(body, `"connected":false`) {
		t.Fatalf("status after revoke = %d %s, want connected=false", code, body)
	}

	// A new grant works again (previous one was consumed/revoked).
	if code, _ := e.do(e.admin, "POST", "/api/local-backend/grant", nil); code != http.StatusCreated {
		t.Fatalf("grant after revoke = %d, want 201", code)
	}
}

func TestLocalBridgeConnectorRejectsBrowserOriginsAndBadGrants(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) { c.ExperimentalLocalBridge = true })
	e.register(e.admin, "anna", "secret1")
	wsURL := "ws" + strings.TrimPrefix(e.api.URL, "http") + "/api/local-backend/connect"

	// Browser-like cross-site Origin must be rejected before grant checks.
	if _, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{e.api.URL}},
	}); err == nil {
		t.Fatal("connector accepted a browser Origin")
	}
	// Unknown bearer is rejected.
	if _, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer not-a-grant"}},
	}); err == nil {
		t.Fatal("connector accepted an unknown grant")
	}
}
