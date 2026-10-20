// Package bridge contains an opt-in outbound connector transport prototype.
// It is deliberately not mounted by cmd serve yet: the Docker Chat/Workspace
// paths remain the production default until pairing, persistence, routing and
// full DSH end-to-end coverage are complete.
package bridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/hashicorp/yamux"
)

const maxGrantTTL = 5 * time.Minute

// helloFrame is the connector's first control message: it announces the
// loopback authority of the local DSH web endpoint so the server-side proxy
// can rewrite Host/Origin and DSH can bind its cookies to a stable authority.
type helloFrame struct {
	LocalAuthority string `json:"localAuthority"`
}

var (
	ErrInvalidGrant = errors.New("bridge: invalid, expired, or already-used grant")
	ErrNoConnector  = errors.New("bridge: no active connector for user")
	ErrBadEndpoint  = errors.New("bridge: connector endpoint must use TLS outside loopback")
	ErrBadLocalAddr = errors.New("bridge: local DSH endpoint must be loopback")
)

type grant struct {
	userID    int64
	expiresAt time.Time
}

type peer struct {
	session   *yamux.Session
	authority string // loopback authority of the connector's local DSH web
}

// Manager owns ephemeral pairing grants and active connector sessions. It is
// process-local for this isolated milestone; durable connector records and
// server-restart semantics are intentionally deferred.
type Manager struct {
	mu      sync.RWMutex
	grants  map[[32]byte]grant
	peers   map[int64]*peer
	changed map[int64]chan struct{}
	now     func() time.Time
}

func NewManager() *Manager {
	return &Manager{
		grants:  make(map[[32]byte]grant),
		peers:   make(map[int64]*peer),
		changed: make(map[int64]chan struct{}),
		now:     time.Now,
	}
}

// IssueGrant creates a short-lived, single-use bearer. Only its SHA-256 hash
// is retained by the Manager; callers must deliver the returned token out of
// band and never put it in a URL or log line.
func (m *Manager) IssueGrant(userID int64, ttl time.Duration) (string, error) {
	if userID <= 0 || ttl <= 0 || ttl > maxGrantTTL {
		return "", fmt.Errorf("bridge: grant requires a user and ttl in (0,%s]", maxGrantTTL)
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("bridge: generate grant: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(secret)
	hash := sha256.Sum256(secret)
	m.mu.Lock()
	issuedAt := m.now()
	for existingHash, existing := range m.grants {
		if existing.userID == userID || !issuedAt.Before(existing.expiresAt) {
			delete(m.grants, existingHash)
		}
	}
	m.grants[hash] = grant{userID: userID, expiresAt: issuedAt.Add(ttl)}
	m.mu.Unlock()
	return token, nil
}

func (m *Manager) consumeGrant(token string) (int64, error) {
	secret, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(secret) != 32 {
		return 0, ErrInvalidGrant
	}
	hash := sha256.Sum256(secret)
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.grants[hash]
	if !ok {
		return 0, ErrInvalidGrant
	}
	delete(m.grants, hash)
	if !m.now().Before(g.expiresAt) {
		return 0, ErrInvalidGrant
	}
	return g.userID, nil
}

// ConnectorHandler accepts the separate bearer-authenticated connector channel.
// The browser's agctl cookie is never accepted as a connector credential.
func (m *Manager) ConnectorHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Origin") != "" {
			// The native connector is not a browser client. Rejecting Origin keeps
			// this bearer-only endpoint out of browser cross-site WebSocket flows.
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		token := bearerToken(r.Header.Get("Authorization"))
		userID, err := m.consumeGrant(token)
		if err != nil {
			http.Error(w, "invalid connector grant", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()

		connCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		carrier := websocket.NetConn(connCtx, ws, websocket.MessageBinary)
		session, err := yamux.Server(carrier, muxConfig())
		if err != nil {
			return
		}
		// The connector must announce its local DSH authority before the peer
		// becomes usable; without it the proxy could not rewrite Host/Origin.
		control, err := session.Accept()
		if err != nil {
			return
		}
		authority, err := readHello(control)
		if err != nil {
			_ = control.Close()
			return
		}
		p := &peer{session: session, authority: authority}
		old := m.register(userID, p)
		if old != nil {
			_ = old.session.Close()
		}
		defer func() {
			m.unregister(userID, p)
			_ = session.Close()
		}()
		<-session.CloseChan()
	})
}

// Connect dials the agctl connector endpoint and serves its multiplexed streams
// by connecting each one to a local loopback DSH web endpoint.
func Connect(ctx context.Context, endpoint, grantToken, localAddr string) error {
	if !isLoopbackAuthority(localAddr) {
		return ErrBadLocalAddr
	}
	if err := validateConnectorURL(endpoint); err != nil {
		return err
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+grantToken)
	ws, _, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		return fmt.Errorf("bridge: dial connector endpoint: %w", err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "connector stopped")

	carrier := websocket.NetConn(ctx, ws, websocket.MessageBinary)
	session, err := yamux.Client(carrier, muxConfig())
	if err != nil {
		return fmt.Errorf("bridge: start multiplexed session: %w", err)
	}
	defer session.Close()

	// Announce the local DSH authority on the control stream before serving
	// any relayed traffic, so the server can register a usable peer.
	control, err := session.Open()
	if err != nil {
		return fmt.Errorf("bridge: open control stream: %w", err)
	}
	if err := writeHello(control, localAddr); err != nil {
		_ = control.Close()
		return fmt.Errorf("bridge: announce local authority: %w", err)
	}
	// Keep the control stream open for the lifetime of the session; yamux has
	// no half-close, and closing it would tear down the whole session.

	stopOnCancel := make(chan struct{})
	defer close(stopOnCancel)
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
		case <-stopOnCancel:
		}
	}()
	for {
		stream, err := session.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, yamux.ErrSessionShutdown) {
				return nil
			}
			return fmt.Errorf("bridge: accept stream: %w", err)
		}
		go relayStream(ctx, stream, localAddr)
	}
}

// Connected reports whether the user has an active connector session.
func (m *Manager) Connected(userID int64) bool {
	m.mu.RLock()
	p := m.peers[userID]
	m.mu.RUnlock()
	return p != nil && !p.session.IsClosed()
}

// WaitConnected is useful to avoid racing an inbound browser request against
// the outbound connector handshake.
func (m *Manager) WaitConnected(ctx context.Context, userID int64) error {
	for {
		m.mu.Lock()
		if p := m.peers[userID]; p != nil && !p.session.IsClosed() {
			m.mu.Unlock()
			return nil
		}
		changed := m.changed[userID]
		if changed == nil {
			changed = make(chan struct{})
			m.changed[userID] = changed
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// ProxyHandler returns an authenticated-caller-only HTTP/SSE/WebSocket proxy to
// the connector's announced local DSH authority. The server route must wrap it
// in requireUser and pass the user ID derived from that session; the browser
// cannot choose this identity.
func (m *Manager) ProxyHandler(userID int64) http.Handler {
	if userID <= 0 {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "local backend unavailable", http.StatusBadGateway)
		})
	}
	target := &url.URL{Scheme: "http", Host: "bridge.invalid"}
	transport := &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return m.openStream(userID)
		},
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			stripPlatformCookies(pr.Out)
			authority, ok := m.BackendAuthority(userID)
			if !ok {
				// Missing authority yields a dial error handled by ErrorHandler.
				pr.Out.URL.Host = "bridge.invalid"
				return
			}
			pr.Out.Host = authority
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", "http://"+authority)
			}
		},
		Transport:     transport,
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, "local backend unavailable", http.StatusBadGateway)
		},
	}
	return proxy
}

// BackendAuthority returns the connector-announced loopback authority.
func (m *Manager) BackendAuthority(userID int64) (string, bool) {
	m.mu.RLock()
	p := m.peers[userID]
	m.mu.RUnlock()
	if p == nil || p.session.IsClosed() || p.authority == "" {
		return "", false
	}
	return p.authority, true
}

func stripPlatformCookies(req *http.Request) {
	cookies := req.Cookies()
	if len(cookies) == 0 {
		return
	}
	req.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == "agctl_session" || strings.HasPrefix(cookie.Name, "agctl_") {
			continue
		}
		req.AddCookie(cookie)
	}
}

// Revoke closes any active connector and forgets all unconsumed grants for the
// user. This is in-memory in the prototype; durable revocation is a later step.
func (m *Manager) Revoke(userID int64) {
	m.mu.Lock()
	for hash, g := range m.grants {
		if g.userID == userID {
			delete(m.grants, hash)
		}
	}
	p := m.peers[userID]
	delete(m.peers, userID)
	m.signalChangedLocked(userID)
	m.mu.Unlock()
	if p != nil {
		_ = p.session.Close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	peers := make([]*peer, 0, len(m.peers))
	for _, p := range m.peers {
		peers = append(peers, p)
	}
	m.peers = make(map[int64]*peer)
	m.grants = make(map[[32]byte]grant)
	m.mu.Unlock()
	for _, p := range peers {
		_ = p.session.Close()
	}
}

func (m *Manager) openStream(userID int64) (net.Conn, error) {
	m.mu.RLock()
	p := m.peers[userID]
	m.mu.RUnlock()
	if p == nil || p.session.IsClosed() {
		return nil, ErrNoConnector
	}
	stream, err := p.session.Open()
	if err != nil {
		return nil, fmt.Errorf("bridge: open connector stream: %w", err)
	}
	return stream, nil
}

func (m *Manager) register(userID int64, p *peer) *peer {
	m.mu.Lock()
	old := m.peers[userID]
	m.peers[userID] = p
	m.signalChangedLocked(userID)
	m.mu.Unlock()
	return old
}

func (m *Manager) unregister(userID int64, p *peer) {
	m.mu.Lock()
	if m.peers[userID] == p {
		delete(m.peers, userID)
		m.signalChangedLocked(userID)
	}
	m.mu.Unlock()
}

func (m *Manager) signalChangedLocked(userID int64) {
	if ch := m.changed[userID]; ch != nil {
		close(ch)
		delete(m.changed, userID)
	}
}

func relayStream(ctx context.Context, stream net.Conn, localAddr string) {
	defer stream.Close()
	local, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", localAddr)
	if err != nil {
		return
	}
	defer local.Close()
	done := make(chan struct{}, 2)
	copyStream := func(dst, src net.Conn) {
		_, _ = io.Copy(dst, src)
		done <- struct{}{}
	}
	go copyStream(local, stream)
	go copyStream(stream, local)
	select {
	case <-ctx.Done():
	case <-done:
	}
}

func muxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = io.Discard
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.ConnectionWriteTimeout = 5 * time.Second
	cfg.MaxStreamWindowSize = 1 << 20
	cfg.StreamOpenTimeout = 15 * time.Second
	cfg.AcceptBacklog = 64
	return cfg
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

// readHello reads the connector's control-frame announcement with a deadline.
func readHello(control net.Conn) (string, error) {
	if err := control.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return "", fmt.Errorf("bridge: control deadline: %w", err)
	}
	var hello helloFrame
	if err := json.NewDecoder(io.LimitReader(control, 4<<10)).Decode(&hello); err != nil {
		return "", fmt.Errorf("bridge: decode hello: %w", err)
	}
	if !isLoopbackAuthority(hello.LocalAuthority) {
		return "", ErrBadLocalAddr
	}
	return hello.LocalAuthority, nil
}

func writeHello(control net.Conn, localAddr string) error {
	if err := control.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return fmt.Errorf("bridge: control deadline: %w", err)
	}
	payload, err := json.Marshal(helloFrame{LocalAuthority: localAddr})
	if err != nil {
		return err
	}
	if _, err := control.Write(append(payload, '\n')); err != nil {
		return err
	}
	return control.SetWriteDeadline(time.Time{})
}

func validateConnectorURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return ErrBadEndpoint
	}
	if u.Scheme == "wss" {
		return nil
	}
	if u.Scheme == "ws" && isLoopbackHost(u.Hostname()) {
		return nil // loopback-only test/local development; deployments must use WSS.
	}
	return ErrBadEndpoint
}

func isLoopbackAuthority(authority string) bool {
	host, port, err := net.SplitHostPort(authority)
	if err != nil {
		return false
	}
	portNumber, err := strconv.Atoi(port)
	return err == nil && portNumber > 0 && portNumber <= 65535 && isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
