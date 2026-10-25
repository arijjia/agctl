package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/store"
	"golang.org/x/crypto/ssh"
)

// ── регистрация / вход ──────────────────────────────────────────────────────

type credsReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func parseSSHPublicKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "", nil
	}
	if len(key) > 4096 {
		return "", errors.New("ключ слишком длинный")
	}
	_, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(key))
	if err != nil || len(strings.TrimSpace(string(rest))) != 0 {
		return "", errors.New("некорректный публичный SSH-ключ")
	}
	return key, nil
}

func (c credsReq) validate() error {
	if len(strings.TrimSpace(c.Login)) < 3 {
		return errors.New("логин: минимум 3 символа")
	}
	if len(c.Password) < 6 {
		return errors.New("пароль: минимум 6 символов")
	}
	return nil
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	if err := req.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := s.db.CountUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	role := "user"
	if n == 0 {
		role = "admin" // первый пользователь — админ
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := s.db.CreateUser(strings.TrimSpace(req.Login), hash, role)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "логин занят")
		return
	}
	_ = s.db.Audit(req.Login, "register", req.Login, "role="+role)
	if err := s.authN.Login(w, id, hash, req.Password, auth.VerifyPassword); err != nil {
		writeErr(w, http.StatusInternalServerError, "регистрация ок, но сессия не создана: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"login": req.Login, "role": role}})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	if !s.authN.AllowLogin(req.Login) {
		writeErr(w, http.StatusTooManyRequests, "слишком много попыток — подождите 5 минут")
		return
	}
	u, err := s.db.UserByLogin(strings.TrimSpace(req.Login))
	if err != nil {
		s.authN.FailLogin(req.Login)
		writeErr(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}
	if err := s.authN.Login(w, u.ID, u.PasswordHash, req.Password, auth.VerifyPassword); err != nil {
		s.authN.FailLogin(req.Login)
		writeErr(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}
	s.authN.ResetLogin(req.Login)
	_ = s.db.Audit(u.Login, "login", u.Login, "")
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"login": u.Login, "role": u.Role}})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.authN.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, err := s.who(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "требуется вход")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"login": id.Login, "role": id.Role}})
}

// ── воркспейсы ──────────────────────────────────────────────────────────────

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request, _ identity) {
	writeJSON(w, http.StatusOK, map[string]any{"templates": provision.Templates})
}

func (s *Server) handleKits(w http.ResponseWriter, r *http.Request, _ identity) {
	writeJSON(w, http.StatusOK, map[string]any{"kits": provision.Kits})
}

func (s *Server) handleWorkspacesList(w http.ResponseWriter, r *http.Request, id identity) {
	var wss []store.Workspace
	var err error
	if id.Role == "admin" {
		wss, err = s.db.WorkspacesAll()
	} else {
		wss, err = s.db.WorkspacesByOwner(id.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(wss))
	for _, ws := range wss {
		owner := id.Login
		if ws.OwnerID != id.ID {
			owner = s.db.WorkspaceOwnerLogin(ws.OwnerID)
		}
		out = append(out, s.wsJSON(r.Context(), ws, owner))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": out})
}

func (s *Server) handleWorkspaceGet(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspace": s.wsJSON(r.Context(), ws, s.db.WorkspaceOwnerLogin(ws.OwnerID))})
}

func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

// provisionJob — файловая часть + запуск контейнера.
func (s *Server) provisionJob(ws store.Workspace, ownerLogin string, p provision.Provider) {
	s.provMu.Lock()
	defer s.provMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	paths := s.pathsFor(ws, ownerLogin)
	fail := func(stage string, err error) {
		_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", stage+": "+err.Error())
		_ = s.db.Audit(ownerLogin, "workspace.fail", ws.ID, stage+": "+err.Error())
	}
	if err := provision.ProvisionWithComponents(paths, p, ws.Template, ws.Components, assetsRepoTemplate, assetsDshSkel); err != nil {
		fail("provision", err)
		return
	}
	_ = s.db.UpdateWorkspacePaths(ws.ID, paths.Repo, paths.Work, paths.DshHome)

	res, err := s.rt.Start(ctx, paths, ws.ID, dockerrt.StartOpts{HostPort: ws.HostPort, SSHPubKey: ws.SSHPubKey, SSHHostPort: sshHostPortFor(ws)}, p)
	if err != nil {
		fail("docker", err)
		return
	}
	if err := s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running"); err != nil {
		fail("db", err)
		return
	}
	// токен dsh web печатается в журнал при первом буте — даём ему время появиться
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if fresh, err := s.db.WorkspaceByID(ws.ID); err == nil {
			if s.wsToken(ctx, fresh) != "" {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	_ = s.db.Audit(ownerLogin, "workspace.running", ws.ID, "port="+strconv.Itoa(res.HostPort))
}

// доступ к встроенным ассетам: сырые FS — provision сам знает префиксы.
var (
	assetsRepoTemplate = assetsRepo // префикс templates/repo
	assetsDshSkel      = assetsSkel // префикс dsh-skel
)

func (s *Server) handleWorkspaceStart(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

func (s *Server) handleWorkspaceStop(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

// purgeWorkspaceFiles удаляет артефакты воркспейса с диска хоста и возвращает
// текст для audit-лога. Каждый путь проверяется: непустой и внутри
// AGCTL_HOME — случайный путь из БД стереть нельзя. Опустевшие
// каталоги-обёртки (work/<id>, repos/<owner>) убираются, только если пустые.
func (s *Server) purgeWorkspaceFiles(ws *store.Workspace) string {
	home, err := filepath.Abs(s.cfg.Home)
	if err != nil {
		home = s.cfg.Home
	}
	var failed []string
	for _, p := range []string{ws.RepoPath, ws.WorkPath, ws.DshHome} {
		if p == "" {
			continue
		}
		if !pathUnder(home, p) {
			failed = append(failed, p+" (вне "+home+")")
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			failed = append(failed, p+": "+err.Error())
		}
	}
	for _, p := range []string{filepath.Dir(ws.WorkPath), filepath.Dir(ws.RepoPath)} {
		if p != "" && p != "." && pathUnder(home, p) {
			_ = os.Remove(p) // сработает только на пустом каталоге
		}
	}
	if len(failed) > 0 {
		log.Printf("[workspace.delete] %s: не всё убрано с диска: %s", ws.ID, strings.Join(failed, "; "))
		return "удалено с ошибками: " + strings.Join(failed, "; ")
	}
	return "удалено вместе с данными на диске"
}

// pathUnder — путь p лежит внутри каталога home (после нормализации).
func pathUnder(home, p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var (
	workspaceLogQueryTokenRE = regexp.MustCompile(`(?i)([?&]token=)[^&\s"'<>]+`)
	workspaceLogBearerRE     = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`)
	workspaceLogEnvKeyRE     = regexp.MustCompile(`(?i)(AGCTL_LLM_API_KEY\s*=\s*)[^\s"'<>]+`)
	workspaceLogCookieRE     = regexp.MustCompile(`(?i)(dsh-auth-[A-Za-z0-9_-]+=)[^;\s]+`)
	workspaceLogAPIKeyRE     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
)

const maxWorkspaceLogTail = 2000

// redactWorkspaceLogs — runtime boot-логи могут включать dsh login token и
// случайно напечатанные credentials; UI/API отдаёт только санитизированный хвост.
func redactWorkspaceLogs(logs string) string {
	logs = workspaceLogQueryTokenRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogBearerRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogEnvKeyRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogCookieRE.ReplaceAllString(logs, `${1}[redacted]`)
	return workspaceLogAPIKeyRE.ReplaceAllString(logs, "[redacted]")
}

func (s *Server) handleWorkspaceLogs(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	if ws.ContainerID == "" {
		writeJSON(w, http.StatusOK, map[string]any{"logs": "(контейнера нет)"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tail := atoiDefault(r.URL.Query().Get("tail"), 200)
	if tail > maxWorkspaceLogTail {
		tail = maxWorkspaceLogTail
	}
	logs, err := s.rt.Logs(ctx, ws.ContainerID, strconv.Itoa(tail))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": redactWorkspaceLogs("(контейнер не отвечает: " + err.Error() + ")")})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": redactWorkspaceLogs(logs)})
}

// sshHostPortFor возвращает опубликованный SSH-порт только для включённого
// SSH-ключа; ноль означает «sshd выключен».
func sshHostPortFor(ws store.Workspace) int {
	if ws.SSHPubKey == "" {
		return 0
	}
	return ws.SSHPort
}

// handleWorkspaceSSH — задать/снять публичный ключ SSH-канала воркспейса
// (концепция v2 §9.2: «агент живёт на сервере, редактор — локально»).
// Если место запущено, контейнер пересоздаётся с новым ключом/портом.
func (s *Server) handleWorkspaceSSH(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

// recreateForSSH применяет ключ/порт к запущенному контейнеру. SSHPubKey
// берётся заново из БД, plaintext-ключ в журнал не попадает.
func (s *Server) recreateForSSH(wsID string, paths provision.Layout, p provision.Provider, actor string) {
	s.provMu.Lock()
	defer s.provMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ws, err := s.db.WorkspaceByID(wsID)
	if err != nil {
		return
	}
	s.forgetToken(wsID)
	if ws.ContainerID != "" {
		_ = s.rt.Remove(ctx, ws.ContainerID)
	}
	res, err := s.rt.Start(ctx, paths, wsID, dockerrt.StartOpts{HostPort: ws.HostPort, SSHPubKey: ws.SSHPubKey, SSHHostPort: sshHostPortFor(ws)}, p)
	if err != nil {
		_ = s.db.UpdateWorkspaceStatus(wsID, "failed", "docker: "+err.Error())
		return
	}
	_ = s.db.UpdateWorkspaceRuntime(wsID, res.ContainerID, res.HostPort, "running")
	_ = s.db.Audit(actor, "workspace.ssh.applied", wsID, fmt.Sprintf("sshPort=%d", ws.SSHPort))
}

// handleWorkspaceProvider — PATCH /api/workspaces/{id}/provider: смена Base URL,
// моделей и токена. Токен возвращается в env контейнера, но никогда в API.
func (s *Server) handleWorkspaceProvider(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

// recreateForProvider — пересоздание контейнера после смены провайдера.
func (s *Server) recreateForProvider(wsID string, paths provision.Layout, p provision.Provider, actor string) {
	s.provMu.Lock()
	defer s.provMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ws, err := s.db.WorkspaceByID(wsID)
	if err != nil {
		return
	}
	s.forgetToken(wsID)
	if ws.ContainerID != "" {
		_ = s.rt.Remove(ctx, ws.ContainerID)
	}
	res, err := s.rt.Start(ctx, paths, wsID, dockerrt.StartOpts{HostPort: ws.HostPort, SSHPubKey: ws.SSHPubKey, SSHHostPort: sshHostPortFor(ws)}, p)
	if err != nil {
		_ = s.db.UpdateWorkspaceStatus(wsID, "failed", "docker: "+err.Error())
		return
	}
	_ = s.db.UpdateWorkspaceRuntime(wsID, res.ContainerID, res.HostPort, "running")
	_ = s.db.Audit(actor, "workspace.provider.applied", wsID, "port="+strconv.Itoa(res.HostPort))
}

// ── админка ─────────────────────────────────────────────────────────────────

func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request, _ identity) {
	users, err := s.db.UsersList()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type u struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Role      string `json:"role"`
		CreatedAt string `json:"createdAt"`
	}
	out := make([]u, 0, len(users))
	for _, x := range users {
		out = append(out, u{ID: x.ID, Login: x.Login, Role: x.Role, CreatedAt: x.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

func (s *Server) handleAdminAudit(w http.ResponseWriter, r *http.Request, _ identity) {
	events, err := s.db.AuditList(atoiDefault(r.URL.Query().Get("limit"), 200))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type e struct {
		TS      string `json:"ts"`
		Actor   string `json:"actor"`
		Action  string `json:"action"`
		Target  string `json:"target"`
		Details string `json:"details"`
	}
	out := make([]e, 0, len(events))
	for _, x := range events {
		out = append(out, e{TS: x.TS, Actor: x.Actor, Action: x.Action, Target: x.Target, Details: x.Details})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

// ── вспомогательное ─────────────────────────────────────────────────────────

func (s *Server) autosaveOne(ws store.Workspace) error {
	if ws.WorkPath == "" {
		return nil
	}
	ok, err := provision.AutosaveOnce(ws.WorkPath)
	if err != nil {
		return err
	}
	if ok {
		log.Printf("[autosave] %s: финальный push выполнен", ws.ID)
	}
	return nil
}

// ── заявки на рабочие места (режим «Разработка», require-approval) ─────────

type wsRequestOut struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kit       string `json:"kit"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	Login     string `json:"login"`
	CreatedAt string `json:"createdAt"`
}

func wsReqJSON(r store.WorkspaceRequest) wsRequestOut {
	return wsRequestOut{ID: r.ID, Name: r.Name, Kit: r.Kit, Status: r.Status,
		Reason: r.Reason, Login: r.Login, CreatedAt: r.CreatedAt}
}

// handleConfig — публичные для авторизованного клиента флаги платформы
// (панель Кабинета подстраивает форму создания места).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"requireApproval": s.cfg.RequireApproval,
		"tier0":           s.cfg.Tier0,
	})
}

// handleWorkspaceRequestCreate — POST /api/workspace-requests.
func (s *Server) handleWorkspaceRequestCreate(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
}

// handleWorkspaceRequestsList — legacy endpoint kept as a tombstone in v3.
func (s *Server) handleWorkspaceRequestsList(w http.ResponseWriter, r *http.Request, id identity) {
	writeErr(w, http.StatusGone, "заявки на отдельные рабочие места демонтированы в v3; используйте /api/role-requests")
}

// handleWorkspaceRequestDecision — POST /api/workspace-requests/{id}/approve|reject (админ).
func (s *Server) handleWorkspaceRequestDecision(approve bool) func(http.ResponseWriter, *http.Request, identity) {
	return func(w http.ResponseWriter, r *http.Request, id identity) {
		writeErr(w, http.StatusGone, "Рабочие места удалены в v3: используйте персональный контейнер и проекты; SSH настраивается через /api/me/ssh.")
	}
}
