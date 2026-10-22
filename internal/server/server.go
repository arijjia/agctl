// Package server — HTTP-сервер agctl: REST API, статика UI, аудит и фоновые
// задачи (autosave, сверка статусов, чистка сессий). Воркспейс-UI доступен
// через шлюз /w/{id}/ — обратный прокси в контейнер с проверкой сессии agctl
// на каждом запросе (см. gateway.go); docker по-прежнему публикует порт
// контейнера только на 127.0.0.1.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/bridge"
	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

// Config — параметры сервера.
type Config struct {
	Home                    string // $AGCTL_HOME
	Addr                    string // слушающий адрес основного UI/API
	MaxPerUser              int
	MemoryBytes             int64
	NanoCPUs                int64
	Image                   string
	BinDir                  string
	SecureCookies           bool // true when agctl is served behind a trusted HTTPS terminator
	AutosaveEvery           time.Duration
	CORSOrigins             []string           // точные origins, которым разрешён кросс-доменный доступ к /api (кабинет-плагин dsh); пусто = CORS выключен
	Tier0                   bool               // корень платформы ведёт в персональный чат (/chat/)
	ChatTTL                 time.Duration      // простой чат-контейнера до сна (по умолчанию 15 мин)
	RequireApproval         bool               // создание dev-мест только через заявку с апрувом админа
	ExperimentalLocalBridge bool               // opt-in pairing/status/revoke API for local connector
	DefaultLLM              provision.Provider // платформенный провайдер чата по умолчанию (корп. LiteLLM); личный секрет пользователя его перекрывает
}

// RuntimeIface — то, что серверу нужно от docker-рантайма (мокается в тестах).
type RuntimeIface interface {
	Start(ctx context.Context, ws provision.Layout, wsID string, opts dockerrt.StartOpts, p provision.Provider) (dockerrt.StartResult, error)
	Resume(ctx context.Context, containerID string) error
	Stop(ctx context.Context, containerID string) error
	Remove(ctx context.Context, containerID string) error
	Logs(ctx context.Context, containerID, tailN string) (string, error)
}

// Server — агрегат всех зависимостей.
type Server struct {
	db    *store.DB
	sec   *secrets.Manager
	authN *auth.Service
	rt    RuntimeIface
	cfg   Config

	provMu sync.Mutex // один provision за раз упрощает порты/гонки
	tokMu  sync.Mutex // кэш boot-токенов dsh web по воркспейсам
	tokens map[string]string

	chatMu       sync.Mutex             // персональные чаты (режим 1): ленивые рантаймы
	chats        map[int64]*chatRuntime // userID → живой runtime handle
	chatLocks    map[int64]*sync.Mutex  // сериализация первого старта per-user
	chatProvider agctlrt.ChatProvider   // Docker now; K8s adapter can return a Service DNS endpoint
	localBridge  *bridge.Manager        // experimental outbound local-backend connector, opt-in routes only
	requestMu    sync.Mutex             // не допускать двойной approve одной заявки

}

// встроенные ассеты для provision (сырые FS; префиксы знает provision).
var (
	assetsRepo = assets.RepoTemplate
	assetsSkel = assets.DshSkel
)

// New builds the development default (Docker for both workspace and Chat
// runtimes). Kubernetes deployments should inject their runtime providers.
func New(db *store.DB, sec *secrets.Manager, cfg Config) (*Server, error) {
	rt, err := dockerrt.New(dockerrt.Config{
		Image:       cfg.Image,
		MemoryBytes: cfg.MemoryBytes,
		NanoCPUs:    cfg.NanoCPUs,
		BinDir:      cfg.BinDir,
		Home:        cfg.Home,
		DshSkel:     assetsSkel,
	}, assets.Image)
	if err != nil {
		return nil, err
	}
	return NewWithRuntimeProviders(db, sec, cfg, rt, rt)
}

// NewWithRuntimeProviders is the injection point for orchestrated deployments.
// ChatProvider is endpoint/storage-neutral; the workspace RuntimeIface still
// needs a Kubernetes adapter before Mode 2 can run without Docker.
func NewWithRuntimeProviders(db *store.DB, sec *secrets.Manager, cfg Config, workspaceRuntime RuntimeIface, chatProvider agctlrt.ChatProvider) (*Server, error) {
	if db == nil || sec == nil || workspaceRuntime == nil || chatProvider == nil {
		return nil, fmt.Errorf("server: db, secrets and both runtime providers are required")
	}
	resolveDefaultLLM(db, sec, &cfg)
	s := &Server{
		db: db, sec: sec, cfg: cfg, rt: workspaceRuntime, chatProvider: chatProvider,
		localBridge: bridge.NewManager(),
		tokens:      map[string]string{}, chats: map[int64]*chatRuntime{}, chatLocks: map[int64]*sync.Mutex{},
	}
	if s.cfg.AutosaveEvery == 0 {
		s.cfg.AutosaveEvery = 2 * time.Minute
	}
	if s.cfg.ChatTTL <= 0 {
		s.cfg.ChatTTL = 15 * time.Minute
	}
	if s.cfg.MaxPerUser <= 0 {
		s.cfg.MaxPerUser = 3
	}
	s.authN = &auth.Service{
		SetSession: func(token string, userID int64, exp time.Time) error {
			return db.CreateSession(token, userID, exp)
		},
		SessionUser: func(token string) (int64, string, string, error) {
			u, err := db.SessionUser(token)
			if err != nil {
				return 0, "", "", err
			}
			return u.ID, u.Login, u.Role, nil
		},
		DeleteSession: db.DeleteSession,
		CookieSecure:  cfg.SecureCookies,
	}
	return s, nil
}

// chatDefaultLLMKey — ключ настроек для платформенного провайдера чата.
const chatDefaultLLMKey = "chat_default_llm"

// resolveDefaultLLM делает платформенный провайдер чата персистентным: задан
// через флаги/env — сохраняется зашифрованным; не задан — восстанавливается из
// прошлых запусков. Так рестарт без AGCTL_LLM_* не возвращает dsh-модал
// «Add an API key». Если провайдера нет вовсе — честно предупреждаем.
func resolveDefaultLLM(db *store.DB, sec *secrets.Manager, cfg *Config) {
	if cfg.DefaultLLM.APIKey != "" {
		plain, err := json.Marshal(cfg.DefaultLLM)
		if err == nil {
			if enc, encErr := sec.Encrypt(string(plain)); encErr == nil {
				if err := db.SettingSet(chatDefaultLLMKey, enc); err != nil {
					log.Printf("llm: не удалось сохранить платформенный провайдер: %v", err)
				}
			}
		}
		return
	}
	raw, err := db.SettingGet(chatDefaultLLMKey)
	if err != nil || raw == "" {
		if cfg.Tier0 {
			log.Printf("llm: предупреждение — платформенный провайдер чата не задан (флаги --llm-* или env AGCTL_LLM_*); dsh покажет модал «Add an API key»")
		}
		return
	}
	plain, err := sec.Decrypt(raw)
	if err != nil {
		log.Printf("llm: не удалось расшифровать сохранённый провайдер чата: %v", err)
		return
	}
	var restored provision.Provider
	if err := json.Unmarshal([]byte(plain), &restored); err != nil || restored.APIKey == "" {
		log.Printf("llm: сохранённый провайдер чата повреждён — игнорирую")
		return
	}
	cfg.DefaultLLM = restored
	log.Printf("llm: восстановлен платформенный провайдер чата %s (моделей: %d, ключ задан) — из сохранённых настроек", restored.BaseURL, len(restored.AllModels()))
}

// Close освобождает живые ленивые chat-контейнеры (тома не удаляются), затем
// закрывает Docker runtime. Dev-workspaces остаются под управлением владельца.
func (s *Server) Close() {
	if s.localBridge != nil {
		s.localBridge.Close()
	}
	s.chatMu.Lock()
	handles := make([]agctlrt.ChatHandle, 0, len(s.chats))
	for _, chat := range s.chats {
		handles = append(handles, chat.handle)
	}
	s.chats = map[int64]*chatRuntime{}
	s.chatMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, handle := range handles {
		s.forgetToken(chatTokenCacheKey(handle))
		if s.chatProvider != nil {
			_ = s.chatProvider.StopChat(ctx, handle)
		}
	}
	if c, ok := s.rt.(interface{ Close() error }); ok {
		_ = c.Close()
	}
}

// ── маршруты ────────────────────────────────────────────────────────────────

// Routes — http.Handler со всеми маршрутами (включая статику SPA).
func (s *Server) Routes(webFS fs.FS) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/register", s.handleRegister)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)
	mux.HandleFunc("GET /api/templates", s.requireUser(s.handleTemplates)) // legacy
	mux.HandleFunc("GET /api/kits", s.requireUser(s.handleKits))
	mux.HandleFunc("GET /api/workspaces", s.requireUser(s.handleWorkspacesList))
	mux.HandleFunc("POST /api/workspaces", s.requireUser(s.handleWorkspaceCreate))
	mux.HandleFunc("GET /api/workspaces/{id}", s.requireUser(s.handleWorkspaceGet))
	mux.HandleFunc("POST /api/workspaces/{id}/start", s.requireUser(s.handleWorkspaceStart))
	mux.HandleFunc("POST /api/workspaces/{id}/stop", s.requireUser(s.handleWorkspaceStop))
	mux.HandleFunc("DELETE /api/workspaces/{id}", s.requireUser(s.handleWorkspaceDelete))
	mux.HandleFunc("GET /api/workspaces/{id}/logs", s.requireUser(s.handleWorkspaceLogs))
	mux.HandleFunc("PATCH /api/workspaces/{id}/provider", s.requireUser(s.handleWorkspaceProvider))
	mux.HandleFunc("PATCH /api/workspaces/{id}/ssh", s.requireUser(s.handleWorkspaceSSH))
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("POST /api/workspace-requests", s.requireUser(s.handleWorkspaceRequestCreate))
	mux.HandleFunc("GET /api/workspace-requests", s.requireUser(s.handleWorkspaceRequestsList))
	mux.HandleFunc("POST /api/workspace-requests/{id}/approve", s.requireAdmin(s.handleWorkspaceRequestDecision(true)))
	mux.HandleFunc("POST /api/workspace-requests/{id}/reject", s.requireAdmin(s.handleWorkspaceRequestDecision(false)))
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("GET /api/admin/audit", s.requireAdmin(s.handleAdminAudit))
	// /map — карта системы за сессией; росток будущей админ-панели (1.md).
	mux.HandleFunc("GET /map", s.requireUser(s.handleMapIndex))
	mux.HandleFunc("GET /map/{name}", s.requireUser(s.handleMapFile))
	// Experimental local-connector control plane. Routes register so the
	// flag-off answer is an honest 404 (not a confusing 405/SPA page); every
	// handler re-checks the flag. The UI proxy below forwards to the local DSH
	// announced by the connector, but real DSH still rejects the first request
	// until boot-token/cookie mint works over the tunnel.
	mux.HandleFunc("POST /api/local-backend/grant", s.requireUser(s.handleLocalBackendGrant))
	mux.HandleFunc("GET /api/local-backend/status", s.requireUser(s.handleLocalBackendStatus))
	mux.HandleFunc("DELETE /api/local-backend", s.requireUser(s.handleLocalBackendRevoke))
	mux.Handle("GET /api/local-backend/connect", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.localBridgeEnabled() {
			http.NotFound(w, r)
			return
		}
		s.handleLocalBackendConnect(w, r)
	}))
	localUI := s.requireUser(func(w http.ResponseWriter, r *http.Request, id identity) {
		if !s.localBridgeEnabled() {
			writeErr(w, http.StatusNotFound, "локальный connector не включён")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !sameOriginAuthority(origin, r.Host) {
			writeErr(w, http.StatusForbidden, "cross-origin запрос к локальному бэку запрещён")
			return
		}
		s.localBridge.ProxyHandler(id.ID).ServeHTTP(w, r)
	})
	mux.Handle("GET /api/local-backend/ui/", http.StripPrefix("/api/local-backend/ui", localUI))
	mux.Handle("POST /api/local-backend/ui/", http.StripPrefix("/api/local-backend/ui", localUI))
	mux.HandleFunc("GET /w/{id}", s.requireUser(s.workspaceProxyEntry))
	// The browser fetches webmanifest without agctl credentials; expose only
	// this non-sensitive static document publicly for a known workspace.
	mux.HandleFunc("GET /w/{id}/manifest.webmanifest", s.workspaceManifest)
	// DSH Web UI uses GET for documents/assets, WebSocket and SSE, and POST
	// for typed RPCs (/api/*). Both methods must reach the reverse proxy.
	// Without POST, ServeMux returns 405 before directoryPicker/list can reach dsh.
	mux.HandleFunc("GET /w/{id}/", s.requireUser(s.workspaceProxy))
	mux.HandleFunc("POST /w/{id}/", s.requireUser(s.workspaceProxy))

	// статика SPA
	if s.cfg.Tier0 {
		// SPA живёт на /app/, корень платформы — общий чат (Tier 0).
		s.registerTier0Routes(mux, webFS)
	}
	mux.Handle("GET /", http.FileServerFS(webFS))
	if len(s.cfg.CORSOrigins) > 0 {
		return s.logMiddleware(s.corsMiddleware(mux))
	}
	return s.logMiddleware(mux)
}

// registerTier0Routes — маршруты режима 1 «Чат». Корень платформы:
// залогинен → /chat/ (личный dsh), нет → /app/ (SPA-логин). SPA на /app/.
func (s *Server) registerTier0Routes(mux *http.ServeMux, webFS fs.FS) {
	mux.HandleFunc("GET /{$}", s.rootEntry)
	mux.HandleFunc("GET /chat", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("GET /chat/", s.requireUser(s.chatProxy))
	mux.HandleFunc("POST /chat/", s.requireUser(s.chatProxy))
	mux.HandleFunc("GET /app", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusPermanentRedirect)
	})
	mux.Handle("GET /app/", http.StripPrefix("/app", http.FileServerFS(webFS)))
}

// corsMiddleware — точный allow-list origins для /api (кабинет-плагин dsh web
// ходит с другого порта/домена). По умолчанию список пуст и middleware не
// ставится: никакого wildcard с credentials никогда не отдаём.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(s.cfg.CORSOrigins))
	for _, o := range s.cfg.CORSOrigins {
		allowed[strings.TrimRight(o, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[strings.TrimRight(origin, "/")] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				h.Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ── фоновые задачи ──────────────────────────────────────────────────────────

// StartBackground запускает autosave, сверку статусов, чистку сессий
// и jira-поллер (если настроен через AGCTL_JIRA_*).
func (s *Server) StartBackground(ctx context.Context) {
	s.StartJiraPoller(ctx)
	// рипер персональных чатов: усыпляем контейнеры по простою (том живёт)
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.reapIdleChats()
			}
		}
	}()
	go func() { // autosave: страховочный commit+push по всем running-воркспейсам
		t := time.NewTicker(s.cfg.AutosaveEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.autosaveAll()
			}
		}
	}()
	go func() { // сверка: контейнер умер, пока agctl был выключен
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.reconcile()
			}
		}
	}()
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.db.DeleteExpiredSessions()
			}
		}
	}()
}

func (s *Server) autosaveAll() {
	wss, err := s.db.WorkspacesAll()
	if err != nil {
		return
	}
	for _, w := range wss {
		if w.Status != "running" || w.WorkPath == "" {
			continue
		}
		if ok, err := provision.AutosaveOnce(w.WorkPath); err != nil {
			log.Printf("[autosave] %s: %v", w.ID, err)
		} else if ok {
			log.Printf("[autosave] %s: закоммичено и отправлено в origin", w.ID)
		}
	}
}

// reconcile помечает running-воркспейсы, чьих контейнеров больше нет.
func (s *Server) reconcile() {
	wss, err := s.db.WorkspacesAll()
	if err != nil {
		return
	}
	for _, w := range wss {
		if w.Status != "running" || w.ContainerID == "" {
			continue
		}
		// грэйс-период: свежезапущенные контейнеры ещё грузятся
		if upd, err := time.Parse("2006-01-02 15:04:05", w.UpdatedAt); err == nil {
			if time.Since(upd) < 2*time.Minute {
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := s.rt.Logs(ctx, w.ContainerID, "1")
		cancel()
		if err != nil {
			_ = s.db.UpdateWorkspaceStatus(w.ID, "stopped", "контейнер не отвечает (сверка)")
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

type apiErr struct {
	Error string `json:"error"`
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(apiErr{Error: msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type identity struct {
	ID    int64
	Login string
	Role  string
}

func (s *Server) who(r *http.Request) (identity, error) {
	id, login, role, err := s.authN.User(r)
	return identity{ID: id, Login: login, Role: role}, err
}

func (s *Server) requireUser(next func(http.ResponseWriter, *http.Request, identity)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.who(r)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "требуется вход")
			return
		}
		next(w, r, id)
	}
}

func (s *Server) requireAdmin(next func(http.ResponseWriter, *http.Request, identity)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, id identity) {
		if id.Role != "admin" {
			writeErr(w, http.StatusForbidden, "нужна роль admin")
			return
		}
		next(w, r, id)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			start := time.Now()
			next.ServeHTTP(w, r)
			log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// workspaceURL — канонический путь UI воркспейса: тот же origin, что и
// кабинет (шлюз /w/{id}/ с сессией agctl). Boot-токен dsh в ссылку не
// попадает: gateway подставляет его серверно при upstream-401 (см. gateway.go).
func workspaceURL(w store.Workspace) string {
	if w.Status != "running" {
		return ""
	}
	return "/w/" + w.ID + "/"
}

var dshTokenRe = regexp.MustCompile(`[?&]token=([A-Za-z0-9_\-]+)`)

// wsToken — boot-токен dev-workspace: из кэша или из runtime-логов.
func (s *Server) wsToken(ctx context.Context, w store.Workspace) string {
	return s.cachedBootToken(ctx, w.ID, func(cctx context.Context) (string, error) {
		if w.ContainerID == "" {
			return "", nil
		}
		return s.rt.Logs(cctx, w.ContainerID, "80")
	})
}

// chatToken uses the same DSH boot-token exchange for any runtime provider;
// the per-instance key prevents a slept/recreated pod/container from reusing
// an earlier generation's token.
func (s *Server) chatToken(ctx context.Context, handle agctlrt.ChatHandle) string {
	if s.chatProvider == nil || handle.InstanceID == "" {
		return ""
	}
	key := chatTokenCacheKey(handle)
	// A Kubernetes Pod may restart its container without changing its Pod UID.
	// Refresh logs on upstream 401 so a new DSH boot token replaces the cached one.
	s.forgetToken(key)
	return s.cachedBootToken(ctx, key, func(cctx context.Context) (string, error) {
		return s.chatProvider.LogsChat(cctx, handle, "80")
	})
}

func (s *Server) cachedBootToken(ctx context.Context, key string, load func(context.Context) (string, error)) string {
	s.tokMu.Lock()
	t := s.tokens[key]
	s.tokMu.Unlock()
	if t != "" {
		return t
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs, err := load(cctx)
	if err != nil {
		return ""
	}
	// берём ПОСЛЕДНЕЕ совпадение: повторный start может оставить старые
	// startup lines выше нового boot token.
	if all := dshTokenRe.FindAllStringSubmatch(logs, -1); len(all) > 0 {
		m := all[len(all)-1]
		s.tokMu.Lock()
		s.tokens[key] = m[1]
		s.tokMu.Unlock()
		return m[1]
	}
	return ""
}

// forgetToken — после рестарта контейнера токен нужно соскрейпить заново.
func (s *Server) forgetToken(wsID string) {
	s.tokMu.Lock()
	delete(s.tokens, wsID)
	s.tokMu.Unlock()
}

func (s *Server) wsJSON(ctx context.Context, w store.Workspace, owner string) map[string]any {
	components := w.Components
	if components == nil {
		components = []string{}
	}
	baseURLs := w.BaseURLs
	if len(baseURLs) == 0 && w.BaseURL != "" {
		baseURLs = []string{w.BaseURL}
	}
	return map[string]any{
		"id": w.ID, "name": w.Name, "status": w.Status,
		"owner": owner, "createdAt": w.CreatedAt, "updatedAt": w.UpdatedAt,
		"url": workspaceURL(w), "port": w.HostPort, "error": w.Error,
		"model": w.Model, "models": w.Models, "baseURL": w.BaseURL, "baseURLs": baseURLs,
		"kit": w.Template, "template": w.Template, "components": components,
		"repoPath": w.RepoPath, "sshEnabled": w.SSHPubKey != "", "sshPort": w.SSHPort,
	}
}

// visibleTo — владелец или админ.
func visibleTo(w store.Workspace, id identity) bool {
	return w.OwnerID == id.ID || id.Role == "admin"
}

// paths для воркспейса от имени владельца.
func (s *Server) pathsFor(w store.Workspace, ownerLogin string) provision.Layout {
	return provision.Paths(s.cfg.Home, ownerLogin, w.ID, w.Name)
}

var errForbidden = errors.New("forbidden")

// loadWS — доступная текущему пользователю запись воркспейса.
func (s *Server) loadWS(r *http.Request, id identity) (store.Workspace, error) {
	wsID := r.PathValue("id")
	w, err := s.db.WorkspaceByID(wsID)
	if err != nil {
		return w, err
	}
	if !visibleTo(w, id) {
		return w, errForbidden
	}
	return w, nil
}

// providerOf — при создании берёт defaults пользователя, при restart —
// настройки конкретного воркспейса; Secret остаётся источником ключа и fallback.
func (s *Server) providerOf(w store.Workspace, ownerID int64, req ProviderReq) (provision.Provider, error) {
	if req.APIKey == "" {
		sec, err := s.db.SecretByUser(ownerID)
		if err != nil {
			return provision.Provider{}, errors.New("нет сохранённого токена — укажите API-токен")
		}
		key, err := s.sec.Decrypt(sec.Ciphertext)
		if err != nil {
			return provision.Provider{}, fmt.Errorf("токен не расшифровывается: %w", err)
		}
		baseURLs, models := w.BaseURLs, w.Models
		if len(baseURLs) == 0 && strings.TrimSpace(w.BaseURL) != "" {
			baseURLs = []string{strings.TrimSpace(w.BaseURL)}
		}
		if len(models) == 0 && w.Model != "" {
			models = []string{w.Model}
		}
		if len(baseURLs) == 0 && strings.TrimSpace(sec.BaseURL) != "" {
			baseURLs = []string{strings.TrimSpace(sec.BaseURL)}
		}
		if len(models) == 0 {
			models = sec.Models
			if len(models) == 0 && sec.Model != "" {
				models = []string{sec.Model}
			}
		}
		if requested := req.allBaseURLs(); len(requested) > 0 {
			baseURLs = requested
		}
		if requested := req.allModels(); len(requested) > 0 {
			models = requested
		}
		base := firstModel(baseURLs)
		return provision.Provider{BaseURL: base, BaseURLs: baseURLs, Model: firstModel(models), Models: models, APIKey: key}, nil
	}
	urls, ms := req.allBaseURLs(), req.allModels()
	return provision.Provider{BaseURL: firstModel(urls), BaseURLs: urls, Model: firstModel(ms), Models: ms, APIKey: req.APIKey}, nil
}

func firstModel(models []string) string {
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

const maxProviderBaseURLs = 8

// ProviderReq — форма создания/редактирования воркспейса. baseURL/model
// остаются scalar aliases для старых клиентов; списки — новые поля.
type ProviderReq struct {
	BaseURL  string   `json:"baseURL"`
	BaseURLs []string `json:"baseURLs"`
	Model    string   `json:"model"`
	Models   []string `json:"models"`
	APIKey   string   `json:"apiKey"`
}

// allBaseURLs — список endpoints в порядке выбора; scalar baseURL дополняет
// list (обычно это first-entry alias) и удаляется как дубль.
func (r ProviderReq) allBaseURLs() []string {
	seen := map[string]bool{}
	var out []string
	add := func(raw string) {
		u := strings.TrimRight(strings.TrimSpace(raw), "/")
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, u := range r.BaseURLs {
		add(u)
	}
	add(r.BaseURL)
	return out
}

// allModels — нормализованный список: models + model, без дублей и пустых.
func (r ProviderReq) allModels() []string {
	seen := map[string]bool{}
	var out []string
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			return
		}
		seen[m] = true
		out = append(out, m)
	}
	for _, m := range r.Models {
		add(m)
	}
	add(r.Model)
	return out
}

func validateBaseURLs(urls []string) error {
	if len(urls) == 0 {
		return errors.New("укажите хотя бы один Base URL провайдера")
	}
	if len(urls) > maxProviderBaseURLs {
		return fmt.Errorf("допускается не больше %d Base URL", maxProviderBaseURLs)
	}
	for _, base := range urls {
		u, err := url.Parse(base)
		if err != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("каждый Base URL должен быть корректным http(s):// URL без логина, пароля, query и fragment")
		}
	}
	return nil
}

func (r ProviderReq) validate() error {
	if err := validateBaseURLs(r.allBaseURLs()); err != nil {
		return err
	}
	if len(r.allModels()) == 0 {
		return errors.New("укажите хотя бы одну модель")
	}
	return nil
}

// saveSecret — запоминаем настройки провайдера (токен — шифротекстом).
func (s *Server) saveSecret(userID int64, p provision.Provider) error {
	cipher, err := s.sec.Encrypt(p.APIKey)
	if err != nil {
		return err
	}
	return s.db.SecretUpsert(userID, p.BaseURL, p.DefaultModel(), p.AllModels(), cipher)
}

func atoiDefault(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

var _ = filepath.Join // keep import in sync for platform-specific files
