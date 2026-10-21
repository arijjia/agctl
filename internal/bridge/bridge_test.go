package bridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testConnectorServer mimics the server-side pairing route: the one-time
// grant is consumed and, when issueCred is set, a durable connector token is
// delivered over the control stream after the upgrade.
func testConnectorServer(m *Manager, issueCred bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		userID, err := m.ConsumeGrant(BearerToken(r.Header.Get("Authorization")))
		if err != nil {
			http.Error(w, "invalid grant", http.StatusUnauthorized)
			return
		}
		token := ""
		if issueCred {
			token = "agctl-conn-1-test"
		}
		m.AcceptConnector(userID, w, r, token)
	})
	return httptest.NewServer(mux)
}

func TestConnectorRejectsBrowserOriginAndOversizedFrames(t *testing.T) {
	m := NewManager()
	defer m.Close()
	grant, err := m.IssueGrant(9, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		userID, err := m.ConsumeGrant(BearerToken(r.Header.Get("Authorization")))
		if err != nil {
			http.Error(w, "invalid grant", http.StatusUnauthorized)
			return
		}
		m.AcceptConnector(userID, w, r, "")
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/connect"

	// Browser-like cross-site Origin never reaches the upgrade.
	if _, _, err := websocket.Dial(context.Background(), endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://evil.example"}},
	}); err == nil {
		t.Fatal("connector accepted a browser Origin")
	}

	// An oversized carrier frame must terminate the session instead of being
	// buffered (coder/websocket rejects reads beyond the configured limit).
	headers := http.Header{"Authorization": []string{"Bearer " + grant}}
	ws, _, err := websocket.Dial(context.Background(), endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	big := make([]byte, (1<<20)+16)
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWrite()
	_ = ws.Write(writeCtx, websocket.MessageBinary, big)
	// Any subsequent exchange must fail: the peer closed on the limit breach.
	readCtx, cancelRead := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRead()
	if _, _, err := ws.Read(readCtx); err == nil {
		t.Fatal("session survived an oversized carrier frame")
	}
}

func TestGrantIsUserBoundSingleUseAndExpires(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	m := NewManager()
	m.now = func() time.Time { return now }

	token, err := m.IssueGrant(42, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := m.ConsumeGrant(token)
	if err != nil || userID != 42 {
		t.Fatalf("consumed grant user=%d err=%v; want user 42", userID, err)
	}
	if _, err := m.ConsumeGrant(token); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("replayed grant err=%v, want ErrInvalidGrant", err)
	}
	if _, err := m.IssueGrant(0, time.Minute); err == nil {
		t.Fatal("grant for user 0 unexpectedly succeeded")
	}
	if _, err := m.IssueGrant(42, maxGrantTTL+time.Second); err == nil {
		t.Fatal("overlong grant unexpectedly succeeded")
	}

	expiring, err := m.IssueGrant(42, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	fresh, err := m.IssueGrant(43, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.grants) != 1 {
		t.Fatalf("expired unconsumed grants were not pruned: remaining=%d", len(m.grants))
	}
	if _, err := m.ConsumeGrant(expiring); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("expired grant err=%v, want ErrInvalidGrant", err)
	}
	if got, err := m.ConsumeGrant(fresh); err != nil || got != 43 {
		t.Fatalf("fresh grant user=%d err=%v; want user 43", got, err)
	}
}

func TestOutboundTunnelProxiesHTTPAndSSEForBoundUser(t *testing.T) {
	var localAuthority string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != localAuthority {
			t.Errorf("upstream Host=%q, want local authority %q", r.Host, localAuthority)
		}
		if got, want := r.Header.Get("Origin"), "http://"+localAuthority; got != "" && got != want {
			t.Errorf("upstream Origin=%q, want %q when forwarded", got, want)
		}
		if r.URL.Path == "/echo" {
			if _, err := r.Cookie("agctl_session"); err == nil {
				t.Error("platform auth cookie leaked to the local backend")
			}
			if cookie, err := r.Cookie("dsh-auth-user"); err != nil || cookie.Value != "runtime-test" {
				t.Errorf("DSH auth cookie not preserved: cookie=%v err=%v", cookie, err)
			}
		}
		switch r.URL.Path {
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			_, _ = io.WriteString(w, "data: tunnel-ok\n\n")
		default:
			body, _ := io.ReadAll(r.Body)
			_, _ = fmt.Fprintf(w, "%s %s host=%s body=%s", r.Method, r.URL.RequestURI(), r.Host, body)
		}
	}))
	defer local.Close()
	localAuthority = strings.TrimPrefix(local.URL, "http://")

	manager := NewManager()
	defer manager.Close()
	connectorServer := testConnectorServer(manager, true)
	defer connectorServer.Close()
	endpoint := "ws" + strings.TrimPrefix(connectorServer.URL, "http") + "/connect"
	grant, err := manager.IssueGrant(17, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	connectorCtx, cancelConnector := context.WithCancel(context.Background())
	defer cancelConnector()
	connectorDone := make(chan error, 1)
	go func() {
		connectorDone <- Connect(connectorCtx, endpoint, grant, ConnectorOptions{LocalAddr: localAuthority})
	}()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	if err := manager.WaitConnected(waitCtx, 17); err != nil {
		t.Fatalf("connector did not establish its outbound session: %v", err)
	}
	proxyServer := httptest.NewServer(manager.ProxyHandler(17))
	defer proxyServer.Close()

	request, err := http.NewRequest(http.MethodPost, proxyServer.URL+"/echo?q=1", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", "https://platform.example.invalid")
	request.Header.Set("Cookie", "agctl_session=platform-test; dsh-auth-user=runtime-test")
	response, err := proxyServer.Client().Do(request)
	if err != nil {
		t.Fatalf("HTTP request through connector: %v", err)
	}
	body, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	want := "POST /echo?q=1 host=" + localAuthority + " body=payload"
	if response.StatusCode != http.StatusOK || string(body) != want {
		t.Fatalf("HTTP tunnel response status=%d body=%q; want 200 %q", response.StatusCode, body, want)
	}

	events, err := proxyServer.Client().Get(proxyServer.URL + "/events")
	if err != nil {
		t.Fatalf("SSE request through connector: %v", err)
	}
	eventBody, readErr := io.ReadAll(events.Body)
	events.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if events.StatusCode != http.StatusOK || !strings.Contains(string(eventBody), "data: tunnel-ok") {
		t.Fatalf("SSE tunnel response status=%d body=%q", events.StatusCode, eventBody)
	}

	wrongUserProxy := httptest.NewServer(manager.ProxyHandler(18))
	defer wrongUserProxy.Close()
	wrongResponse, err := wrongUserProxy.Client().Get(wrongUserProxy.URL + "/echo")
	if err != nil {
		t.Fatal(err)
	}
	wrongResponse.Body.Close()
	if wrongResponse.StatusCode != http.StatusBadGateway {
		t.Fatalf("unpaired user status=%d, want 502", wrongResponse.StatusCode)
	}

	manager.Revoke(17)
	select {
	case <-connectorDone:
	case <-time.After(5 * time.Second):
		t.Fatal("revoking connector did not close its outbound session")
	}
	if _, err := manager.ConsumeGrant(grant); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("consumed grant became reusable after revoke: %v", err)
	}
}

func TestOutboundTunnelMintsDSHBootTokenAndAcceptsRefresh(t *testing.T) {
	var mu sync.Mutex
	seenTokens := make([]string, 0, 4) // token param of each upstream hit
	valid := "boot-secret-1"
	var localAuthority string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenTokens = append(seenTokens, r.URL.Query().Get("token"))
		current := valid
		mu.Unlock()
		if r.URL.Query().Get("token") != current {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Location", "/")
		w.WriteHeader(http.StatusSeeOther)
	}))
	defer local.Close()
	localAuthority = strings.TrimPrefix(local.URL, "http://")

	manager := NewManager()
	defer manager.Close()
	connectorServer := testConnectorServer(manager, true)
	defer connectorServer.Close()
	endpoint := "ws" + strings.TrimPrefix(connectorServer.URL, "http") + "/connect"
	grant, err := manager.IssueGrant(31, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	tokenUpdates := make(chan string, 1)
	connectorCtx, cancelConnector := context.WithCancel(context.Background())
	defer cancelConnector()
	connectorDone := make(chan error, 1)
	go func() {
		connectorDone <- Connect(connectorCtx, endpoint, grant, ConnectorOptions{
			LocalAddr:     localAuthority,
			DSHBootTokens: tokenUpdates,
		})
	}()
	// hello frame announces the current boot token.
	tokenUpdates <- "boot-secret-1"

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	if err := manager.WaitConnected(waitCtx, 31); err != nil {
		t.Fatalf("connector did not establish its outbound session: %v", err)
	}
	proxyServer := httptest.NewServer(manager.ProxyHandler(31))
	defer proxyServer.Close()
	proxyServer.Client().CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// Mint retry must use the connector-announced token server-side only.
	response, err := proxyServer.Client().Get(proxyServer.URL + "/?also=1")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	mu.Lock()
	firstSeen := append([]string(nil), seenTokens...)
	mu.Unlock()
	if response.StatusCode != http.StatusSeeOther || len(firstSeen) != 2 || firstSeen[0] != "" || firstSeen[1] != "boot-secret-1" {
		t.Fatalf("mint via hello: status=%d seen=%v; want 303 [\"\", boot-secret-1]", response.StatusCode, firstSeen)
	}
	if got := response.Header.Get("Location"); got != "/" {
		t.Fatalf("minted Location=%q, want /", got)
	}

	// A refreshed boot token (local DSH restart) replaces the stored one.
	mu.Lock()
	valid = "boot-secret-2"
	mu.Unlock()
	tokenUpdates <- "boot-secret-2"
	refreshDeadline := time.Now().Add(3 * time.Second)
	for manager.DSHToken(31) != "boot-secret-2" {
		if time.Now().After(refreshDeadline) {
			t.Fatalf("token refresh not applied; current=%q", manager.DSHToken(31))
		}
		time.Sleep(20 * time.Millisecond)
	}
	response2, err := proxyServer.Client().Get(proxyServer.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	response2.Body.Close()
	mu.Lock()
	latest := seenTokens[len(seenTokens)-1]
	mu.Unlock()
	if response2.StatusCode != http.StatusSeeOther || latest != "boot-secret-2" {
		t.Fatalf("mint via refresh: status=%d latest token=%q; want 303 boot-secret-2", response2.StatusCode, latest)
	}

	manager.Revoke(31)
	select {
	case <-connectorDone:
	case <-time.After(5 * time.Second):
		t.Fatal("revoking connector did not close its outbound session")
	}
}

func TestOutboundTunnelPreservesWebSocketUpgrade(t *testing.T) {
	var localAuthority string
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != localAuthority || r.Header.Get("Origin") != "http://"+localAuthority {
			http.Error(w, "upstream Host/Origin mismatch", http.StatusForbidden)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		kind, message, err := ws.Read(ctx)
		if err != nil {
			return
		}
		_ = ws.Write(ctx, kind, append([]byte("echo:"), message...))
	}))
	defer local.Close()
	localAuthority = strings.TrimPrefix(local.URL, "http://")

	manager := NewManager()
	defer manager.Close()
	connectorServer := testConnectorServer(manager, true)
	defer connectorServer.Close()
	endpoint := "ws" + strings.TrimPrefix(connectorServer.URL, "http") + "/connect"
	grant, err := manager.IssueGrant(25, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	connectorCtx, cancelConnector := context.WithCancel(context.Background())
	defer cancelConnector()
	connectorDone := make(chan error, 1)
	go func() {
		connectorDone <- Connect(connectorCtx, endpoint, grant, ConnectorOptions{LocalAddr: localAuthority})
	}()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	if err := manager.WaitConnected(waitCtx, 25); err != nil {
		t.Fatalf("connector did not establish its outbound session: %v", err)
	}
	proxyServer := httptest.NewServer(manager.ProxyHandler(25))
	defer proxyServer.Close()

	headers := make(http.Header)
	headers.Set("Origin", "https://platform.example.invalid")
	browserURL := "ws" + strings.TrimPrefix(proxyServer.URL, "http") + "/api/remote.mux"
	browserConn, _, err := websocket.Dial(context.Background(), browserURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("WebSocket Upgrade through outbound tunnel: %v", err)
	}
	defer browserConn.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := browserConn.Write(ctx, websocket.MessageText, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	kind, message, err := browserConn.Read(ctx)
	if err != nil {
		t.Fatalf("read tunneled WebSocket reply: %v", err)
	}
	if kind != websocket.MessageText || string(message) != "echo:ping" {
		t.Fatalf("WebSocket reply type=%v body=%q", kind, message)
	}
	manager.Revoke(25)
	select {
	case <-connectorDone:
	case <-time.After(5 * time.Second):
		t.Fatal("revoking connector did not close its WebSocket tunnel")
	}
}
