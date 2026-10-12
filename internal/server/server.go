// Package server — HTTP-сервер agctl: REST API, статика UI, перенаправление
// в воркспейсы, аудит и фоновые задачи (autosave, сверка статусов, чистка
// сессий). Воркспейс-UI отдаётся docker'ом на 127.0.0.1:<port>; /w/{id} —
// авторизованный редирект на этот порт.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/secrets"
	"github.com/arijjia/agctl/internal/store"
)

// Config — параметры сервера.
type Config struct {
	Home          string // $AGCTL_HOME
	Addr          string // слушающий адрес основного UI/API
	MaxPerUser    int
	MemoryBytes   int64
	NanoCPUs      int64
	Image         string
	BinDir        string
	AutosaveEvery time.Duration
}

// RuntimeIface — то, что серверу нужно от docker-рантайма (мокается в тестах).
type RuntimeIface interface {
	Start(ctx context.Context, ws provision.Layout, wsID string, hostPort int, p provision.Provider) (dockerrt.StartResult, error)
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
}

// встроенные ассеты для provision (сырые FS; префиксы знает provision).
var (
	assetsRepo = assets.RepoTemplate
	assetsSkel = assets.DshSkel
)

// New собирает сервер: docker-рантайм (если доступен), ассеты, auth.
func New(db *store.DB, sec *secrets.Manager, cfg Config) (*Server, error) {
	s := &Server{db: db, sec: sec, cfg: cfg, tokens: map[string]string{}}
	if s.cfg.AutosaveEvery == 0 {
		s.cfg.AutosaveEvery = 2 * time.Minute
	}
	if s.cfg.MaxPerUser <= 0 {
		s.cfg.MaxPerUser = 3
	}
	rt, err := dockerrt.New(dockerrt.Config{
		Image:       cfg.Image,
		MemoryBytes: cfg.MemoryBytes,
		NanoCPUs:    cfg.NanoCPUs,
		BinDir:      cfg.BinDir,
	}, assets.Image)
	if err != nil {
		return nil, err
	}
	s.rt = rt
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
	}
	return s, nil
}

// Close освобождает рантайм.
func (s *Server) Close() {
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
	mux.HandleFunc("GET /api/templates", s.requireUser(s.handleTemplates))
	mux.HandleFunc("GET /api/workspaces", s.requireUser(s.handleWorkspacesList))
	mux.HandleFunc("POST /api/workspaces", s.requireUser(s.handleWorkspaceCreate))
	mux.HandleFunc("GET /api/workspaces/{id}", s.requireUser(s.handleWorkspaceGet))
	mux.HandleFunc("POST /api/workspaces/{id}/start", s.requireUser(s.handleWorkspaceStart))
	mux.HandleFunc("POST /api/workspaces/{id}/stop", s.requireUser(s.handleWorkspaceStop))
	mux.HandleFunc("DELETE /api/workspaces/{id}", s.requireUser(s.handleWorkspaceDelete))
	mux.HandleFunc("GET /api/workspaces/{id}/logs", s.requireUser(s.handleWorkspaceLogs))
	mux.HandleFunc("PATCH /api/workspaces/{id}/provider", s.requireUser(s.handleWorkspaceProvider))
	mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.handleAdminUsers))
	mux.HandleFunc("GET /api/admin/audit", s.requireAdmin(s.handleAdminAudit))
	mux.HandleFunc("GET /w/{id}", s.requireUser(s.handleWorkspaceRedirect))

	// статика SPA
	mux.Handle("GET /", http.FileServerFS(webFS))
	return s.logMiddleware(mux)
}

// ── фоновые задачи ──────────────────────────────────────────────────────────

// StartBackground запускает autosave, сверку статусов, чистку сессий
// и jira-поллер (если настроен через AGCTL_JIRA_*).
func (s *Server) StartBackground(ctx context.Context) {
	s.StartJiraPoller(ctx)
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

// workspaceURL — ссылка на UI воркспейса (docker публикует на loopback).
func workspaceURL(w store.Workspace) string {
	if w.Status != "running" || w.HostPort == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", w.HostPort)
}

var dshTokenRe = regexp.MustCompile(`[?&]token=([A-Za-z0-9_\-]+)`)

// wsToken — boot-токен dsh web воркспейса: из кэша или из журнала контейнера
// (dsh печатает ссылку с ?token=… при старте). Пусто — если не нашли.
func (s *Server) wsToken(ctx context.Context, w store.Workspace) string {
	s.tokMu.Lock()
	t := s.tokens[w.ID]
	s.tokMu.Unlock()
	if t != "" {
		return t
	}
	if w.ContainerID == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	logs, err := s.rt.Logs(cctx, w.ContainerID, "80")
	if err != nil {
		return ""
	}
	// берём ПОСЛЕДНЕЕ совпадение: при docker start того же контейнера dsh
	// печатает новый boot-токен, а старые строки остаются в журнале выше
	// (баг-фикс: первый match отдавал устаревший токен → 401 по «Открыть»)
	if all := dshTokenRe.FindAllStringSubmatch(logs, -1); len(all) > 0 {
		m := all[len(all)-1]
		s.tokMu.Lock()
		s.tokens[w.ID] = m[1]
		s.tokMu.Unlock()
		return m[1]
	}
	return ""
}

// openURL — готовая ссылка «Открыть» (с токеном dsh, если он уже известен).
func (s *Server) openURL(ctx context.Context, w store.Workspace) string {
	base := workspaceURL(w)
	if base == "" {
		return ""
	}
	if tok := s.wsToken(ctx, w); tok != "" {
		return base + "?token=" + tok
	}
	return base
}

// forgetToken — после рестарта контейнера токен нужно соскрейпить заново.
func (s *Server) forgetToken(wsID string) {
	s.tokMu.Lock()
	delete(s.tokens, wsID)
	s.tokMu.Unlock()
}

func (s *Server) wsJSON(ctx context.Context, w store.Workspace, owner string) map[string]any {
	return map[string]any{
		"id": w.ID, "name": w.Name, "status": w.Status,
		"owner": owner, "createdAt": w.CreatedAt, "updatedAt": w.UpdatedAt,
		"url": s.openURL(ctx, w), "port": w.HostPort, "error": w.Error,
		"model": w.Model, "models": w.Models, "baseURL": w.BaseURL, "template": w.Template,
		"repoPath": w.RepoPath,
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

// providerOf — провайдер воркспейса: настройки из запроса или из секрета юзера.
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
		base, models := sec.BaseURL, sec.Models
		if req.BaseURL != "" {
			base = req.BaseURL
		}
		if ms := req.allModels(); len(ms) > 0 {
			models = ms
		}
		return provision.Provider{BaseURL: base, Model: firstModel(models), Models: models, APIKey: key}, nil
	}
	ms := req.allModels()
	return provision.Provider{BaseURL: req.BaseURL, Model: firstModel(ms), Models: ms, APIKey: req.APIKey}, nil
}

func firstModel(models []string) string {
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// ProviderReq — форма создания/старта воркспейса.
// models — список (по одной на строку допускается), model — legacy/первая.
type ProviderReq struct {
	BaseURL string   `json:"baseURL"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
	APIKey  string   `json:"apiKey"`
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

func (r ProviderReq) validate() error {
	if strings.TrimSpace(r.BaseURL) == "" {
		return errors.New("укажите Base URL провайдера")
	}
	if len(r.allModels()) == 0 {
		return errors.New("укажите хотя бы одну модель")
	}
	if !strings.Contains(r.BaseURL, "://") {
		return errors.New("Base URL должен начинаться с http(s)://")
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
