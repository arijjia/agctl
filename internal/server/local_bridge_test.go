package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/arijjia/agctl/internal/bridge"
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

	// Connector handshake consumes the grant (Authorization bearer only) and
	// announces its loopback authority via the control frame.
	fakeLocal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fakeLocal.Close()
	connectorCtx, cancelConnector := context.WithCancel(context.Background())
	defer cancelConnector()
	connectorDone := make(chan error, 1)
	go func() {
		connectorDone <- bridge.Connect(connectorCtx,
			"ws"+strings.TrimPrefix(e.api.URL, "http")+granted.ConnectPath,
			granted.Grant, bridge.ConnectorOptions{LocalAddr: strings.TrimPrefix(fakeLocal.URL, "http://")})
	}()

	// Status flips to connected for this user's session once the grant is
	// consumed and the hello frame is processed.
	wsURL := "ws" + strings.TrimPrefix(e.api.URL, "http") + granted.ConnectPath
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

	// Replay with the same one-time grant must fail; close if it did not.
	if replay, _, err := websocket.Dial(context.Background(), wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + granted.Grant}},
	}); err == nil {
		replay.CloseNow()
		t.Fatal("grant replay unexpectedly succeeded")
	}

	// Revoke closes the connector session and invalidates further pairing.
	if code, _ := e.do(e.admin, "DELETE", "/api/local-backend", nil); code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204", code)
	}
	select {
	case <-connectorDone:
	case <-time.After(5 * time.Second):
		t.Fatal("revoking connector did not close its outbound session")
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

func TestLocalBridgeUIProxyRoutesToConnectorAnnouncedBackend(t *testing.T) {
	var localAuthority string
	var seenHost, seenOrigin string
	local := http.NewServeMux()
	local.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		seenHost, seenOrigin = r.Host, r.Header.Get("Origin")
		_, _ = w.Write([]byte("local-echo " + r.Method + " " + r.URL.RequestURI()))
	})
	localServer := http.Server{Handler: local}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	localAuthority = listener.Addr().String()
	go func() { _ = localServer.Serve(listener) }()
	defer localServer.Close()

	e := newTestEnvWith(t, 3, func(c *Config) { c.ExperimentalLocalBridge = true })
	e.register(e.admin, "anna", "secret1")

	code, body := e.do(e.admin, "POST", "/api/local-backend/grant", nil)
	if code != http.StatusCreated {
		t.Fatalf("grant = %d %s", code, body)
	}
	var granted localGrantResponse
	if err := json.Unmarshal([]byte(body), &granted); err != nil {
		t.Fatal(err)
	}

	connectorCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	connectorDone := make(chan error, 1)
	go func() {
		connectorDone <- bridge.Connect(connectorCtx,
			"ws"+strings.TrimPrefix(e.api.URL, "http")+granted.ConnectPath,
			granted.Grant, bridge.ConnectorOptions{LocalAddr: localAuthority})
	}()
	defer func() {
		cancel()
		select {
		case <-connectorDone:
		case <-time.After(5 * time.Second):
			t.Error("connector did not stop after context cancel")
		}
	}()

	// Status flips to connected once the hello frame is processed.
	deadline := time.Now().Add(5 * time.Second)
	for {
		code, body = e.do(e.admin, "GET", "/api/local-backend/status", nil)
		if code == http.StatusOK && strings.Contains(body, `"connected":true`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("connector never connected; last body=%s", body)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// UI proxy reaches the connector-announced loopback backend for the owner.
	ownerRequest, err := http.NewRequest(http.MethodGet, e.api.URL+"/api/local-backend/ui/echo?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	ownerRequest.Header.Set("Origin", e.api.URL)
	ownerResponse, err := e.admin.Do(ownerRequest)
	if err != nil {
		t.Fatal(err)
	}
	ownerBody, readErr := io.ReadAll(ownerResponse.Body)
	ownerResponse.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if ownerResponse.StatusCode != http.StatusOK || string(ownerBody) != "local-echo GET /echo?x=1" {
		t.Fatalf("owner UI proxy = %d %q; want 200 local echo", ownerResponse.StatusCode, ownerBody)
	}
	if seenHost != localAuthority {
		t.Fatalf("upstream Host=%q, want connector-announced %q", seenHost, localAuthority)
	}
	if seenOrigin != "http://"+localAuthority {
		t.Fatalf("upstream Origin=%q, want rewritten %q", seenOrigin, "http://"+localAuthority)
	}

	// A different user has no connector: the proxy degrades to 502.
	bob := e.newClient()
	e.register(bob, "bob", "secret2")
	if code, _ := e.do(bob, "GET", "/api/local-backend/ui/echo", nil); code != http.StatusBadGateway {
		t.Fatalf("other-user UI proxy = %d, want 502", code)
	}
}

func TestLocalBridgeUIProxyDisabledWithoutFlag(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	if code, _ := e.do(e.admin, "GET", "/api/local-backend/ui/echo", nil); code != http.StatusNotFound {
		t.Fatalf("UI proxy without experimental flag = %d, want 404", code)
	}
}
