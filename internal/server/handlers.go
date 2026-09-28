package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/store"
)

// ── регистрация / вход ──────────────────────────────────────────────────────

type credsReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
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
		out = append(out, wsJSON(ws, owner))
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
	writeJSON(w, http.StatusOK, map[string]any{"workspace": wsJSON(ws, s.db.WorkspaceOwnerLogin(ws.OwnerID))})
}

func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request, id identity) {
	var req struct {
		Name string `json:"name"`
		ProviderReq
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len(req.Name) < 2 {
		writeErr(w, http.StatusBadRequest, "имя воркспейса: минимум 2 символа")
		return
	}
	if err := req.ProviderReq.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// квота на число воркспейсов
	own, err := s.db.WorkspacesByOwner(id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(own) >= s.cfg.MaxPerUser {
		writeErr(w, http.StatusBadRequest, "достигнут лимит воркспейсов ("+strconv.Itoa(s.cfg.MaxPerUser)+")")
		return
	}

	p, err := s.providerOf(store.Workspace{}, id.ID, req.ProviderReq)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	wsID, err := provision.NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "нет свободного порта: "+err.Error())
		return
	}
	ws := store.Workspace{
		ID: wsID, OwnerID: id.ID, Name: req.Name, Status: "provisioning",
		HostPort: port, BaseURL: p.BaseURL, Model: p.Model,
	}
	if err := s.db.InsertWorkspace(ws); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.saveSecret(id.ID, p); err != nil {
		writeErr(w, http.StatusInternalServerError, "секрет не сохранён: "+err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "workspace.create", wsID, "name="+req.Name)

	// provision идёт в фоне: UI опрашивает статус («колесико»)
	go s.provisionJob(ws, id.Login, p)
	ws, _ = s.db.WorkspaceByID(wsID)
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": wsJSON(ws, id.Login)})
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
	if err := provision.Provision(paths, p, assetsRepoTemplate, assetsDshSkel); err != nil {
		fail("provision", err)
		return
	}
	_ = s.db.UpdateWorkspacePaths(ws.ID, paths.Repo, paths.Work, paths.DshHome)

	res, err := s.rt.Start(ctx, paths, ws.ID, ws.HostPort, p)
	if err != nil {
		fail("docker", err)
		return
	}
	if err := s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running"); err != nil {
		fail("db", err)
		return
	}
	_ = s.db.Audit(ownerLogin, "workspace.running", ws.ID, "port="+strconv.Itoa(res.HostPort))
}

// доступ к встроенным ассетам без цикла импортов: инициализируются в main.
var (
	assetsRepoTemplate = mustSub(assetsRepo, "templates/repo")
	assetsDshSkel      = mustSub(assetsSkel, "dsh-skel")
)

func (s *Server) handleWorkspaceStart(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	if ws.Status == "running" {
		writeJSON(w, http.StatusOK, map[string]any{"workspace": wsJSON(ws, id.Login)})
		return
	}
	if ws.Status == "provisioning" {
		writeErr(w, http.StatusConflict, "воркспейс ещё создаётся")
		return
	}
	p, err := s.providerOf(ws, ws.OwnerID, ProviderReq{})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	go func() {
		s.provMu.Lock()
		defer s.provMu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if ws.ContainerID != "" {
			// контейнер остался (стоп без удаления) — просто стартуем
			if err := s.rt.Resume(ctx, ws.ContainerID); err != nil {
				// контейнера нет — создаём заново на сохранённом порту
				res, err2 := s.rt.Start(ctx, s.pathsFor(ws, id.Login), ws.ID, ws.HostPort, p)
				if err2 != nil {
					_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", "docker: "+err2.Error())
					return
				}
				_ = s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running")
			} else {
				_ = s.db.UpdateWorkspaceStatus(ws.ID, "running", "")
			}
		} else {
			res, err2 := s.rt.Start(ctx, s.pathsFor(ws, s.db.WorkspaceOwnerLogin(ws.OwnerID)), ws.ID, ws.HostPort, p)
			if err2 != nil {
				_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", "docker: "+err2.Error())
				return
			}
			_ = s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running")
		}
		_ = s.db.Audit(id.Login, "workspace.start", ws.ID, "")
		// страховочный push перед стартом не нужен, но после старта — да
		_ = s.autosaveOne(ws)
	}()
	ws, _ = s.db.WorkspaceByID(ws.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": wsJSON(ws, id.Login)})
}

func (s *Server) handleWorkspaceStop(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	go func() {
		if ws.ContainerID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			_ = s.autosaveOne(ws) // финальный страховочный push
			if err := s.rt.Stop(ctx, ws.ContainerID); err != nil {
				_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", "docker stop: "+err.Error())
				return
			}
		}
		_ = s.db.UpdateWorkspaceStatus(ws.ID, "stopped", "")
		_ = s.db.Audit(id.Login, "workspace.stop", ws.ID, "")
	}()
	ws.Status = "stopping"
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": wsJSON(ws, id.Login)})
}

func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	go func() {
		if ws.ContainerID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
			defer cancel()
			_ = s.autosaveOne(ws)
			_ = s.rt.Remove(ctx, ws.ContainerID)
		}
		// файлы воркспейса (репка+клон) остаются на диске — данные дороже места
		_ = s.db.DeleteWorkspace(ws.ID)
		_ = s.db.Audit(id.Login, "workspace.delete", ws.ID, "файлы на диске сохранены")
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{})
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
	logs, err := s.rt.Logs(ctx, ws.ContainerID, strconv.Itoa(atoiDefault(r.URL.Query().Get("tail"), 200)))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": "(контейнер не отвечает: " + err.Error() + ")"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

// handleWorkspaceRedirect — /w/{id}: авторизованный переход в UI воркспейса.
func (s *Server) handleWorkspaceRedirect(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	url := workspaceURL(ws)
	if url == "" {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
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
