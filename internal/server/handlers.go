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
	if s.cfg.RequireApproval {
		writeErr(w, http.StatusForbidden, "включён режим апрува: создайте заявку через /api/workspace-requests")
		return
	}
	var req struct {
		Name       string   `json:"name"`
		Kit        string   `json:"kit"`
		Template   string   `json:"template"` // legacy clients
		Components []string `json:"components"`
		SSHPubKey  string   `json:"sshPubKey"`
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
	requestedKit := req.Kit
	if requestedKit == "" {
		requestedKit = req.Template // compatibility with older clients
	}
	kitID, components, err := provision.NormalizeComponents(requestedKit, req.Components)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.ProviderReq.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sshPubKey, err := parseSSHPublicKey(req.SSHPubKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sshPort := 0
	if sshPubKey != "" {
		sshPort, err = dockerrt.FreePort()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "нет свободного SSH-порта: "+err.Error())
			return
		}
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
	// имя занято файлами воркспейса, удалённого до 2026-10-14 (тогда удаление
	// оставляло файлы на диске) — понятная ошибка вместо падения provision
	// на «bare init»; с 2026-10-14 удаление чистит диск само
	paths := provision.Paths(s.cfg.Home, id.Login, wsID, req.Name)
	if _, err := os.Stat(paths.Repo); err == nil {
		writeErr(w, http.StatusBadRequest,
			"репозиторий «"+req.Name+"» уже есть на диске (остался от удалённого рабочего места): выберите другое имя или удалите "+paths.Repo)
		return
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "нет свободного порта: "+err.Error())
		return
	}
	ws := store.Workspace{
		ID: wsID, OwnerID: id.ID, Name: req.Name, Template: kitID, Components: components,
		Status: "provisioning", HostPort: port, BaseURL: p.BaseURL, BaseURLs: p.AllBaseURLs(),
		Model: p.DefaultModel(), Models: p.AllModels(), SSHPubKey: sshPubKey, SSHPort: sshPort,
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
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": s.wsJSON(r.Context(), ws, id.Login)})
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
		writeJSON(w, http.StatusOK, map[string]any{"workspace": s.wsJSON(r.Context(), ws, id.Login)})
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
		s.forgetToken(ws.ID)
		if ws.ContainerID != "" {
			// стоп теперь удаляет контейнер (см. dockerrt.Stop); Resume нужен
			// для контейнеров, переживших падение демона/serve или оставшихся
			// от старой версии. Resume упал — создаём заново на сохранённом
			// порту с актуальной конфигурацией (пин хоста провайдера и т.п.)
			if err := s.rt.Resume(ctx, ws.ContainerID); err != nil {
				// контейнера нет — создаём заново на сохранённом порту
				res, err2 := s.rt.Start(ctx, s.pathsFor(ws, id.Login), ws.ID, dockerrt.StartOpts{HostPort: ws.HostPort, SSHPubKey: ws.SSHPubKey, SSHHostPort: sshHostPortFor(ws)}, p)
				if err2 != nil {
					_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", "docker: "+err2.Error())
					return
				}
				_ = s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running")
			} else {
				_ = s.db.UpdateWorkspaceStatus(ws.ID, "running", "")
			}
		} else {
			res, err2 := s.rt.Start(ctx, s.pathsFor(ws, s.db.WorkspaceOwnerLogin(ws.OwnerID)), ws.ID, dockerrt.StartOpts{HostPort: ws.HostPort, SSHPubKey: ws.SSHPubKey, SSHHostPort: sshHostPortFor(ws)}, p)
			if err2 != nil {
				_ = s.db.UpdateWorkspaceStatus(ws.ID, "failed", "docker: "+err2.Error())
				return
			}
			_ = s.db.UpdateWorkspaceRuntime(ws.ID, res.ContainerID, res.HostPort, "running")
		}
		time.Sleep(3 * time.Second) // ждём печать boot-ссылки dsh
		_ = s.wsToken(ctx, ws)
		_ = s.db.Audit(id.Login, "workspace.start", ws.ID, "")
		// страховочный push перед стартом не нужен, но после старта — да
		_ = s.autosaveOne(ws)
	}()
	ws, _ = s.db.WorkspaceByID(ws.ID)
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": s.wsJSON(r.Context(), ws, id.Login)})
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
	ws.Status = "stopping" // до запуска горутины: иначе гонка на чтении ws внутри неё
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
	writeJSON(w, http.StatusAccepted, map[string]any{"workspace": s.wsJSON(r.Context(), ws, id.Login)})
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
		if err := s.db.DeleteWorkspace(ws.ID); err != nil {
			log.Printf("[workspace.delete] %s: бд: %v", ws.ID, err)
		}
		// решение владельца 2026-10-14: удалённый воркспейс уносит с диска
		// и свои данные (bare-репка, клон, дом dsh) — прежнее «файлы остаются,
		// данные дороже места» давало конфликт «уже есть на диске» при
		// пересоздании воркспейса с тем же именем
		_ = s.db.Audit(id.Login, "workspace.delete", ws.ID, s.purgeWorkspaceFiles(&ws))
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{})
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
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	if ws.Status == "provisioning" {
		writeErr(w, http.StatusConflict, "место ещё запускается; повтори запрос после статуса running")
		return
	}
	var req struct {
		PubKey string `json:"pubKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	key, err := parseSSHPublicKey(req.PubKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	port := ws.SSHPort
	switch {
	case key == "":
		port = 0
	case port == 0:
		if port, err = dockerrt.FreePort(); err != nil {
			writeErr(w, http.StatusInternalServerError, "нет свободного порта: "+err.Error())
			return
		}
	}

	// Для работающего места заранее собираем текущий provider, чтобы при ошибке
	// не сохранить SSH-ключ, который невозможно применить пересозданием.
	var provider provision.Provider
	if ws.Status == "running" {
		sec, secErr := s.db.SecretByUser(ws.OwnerID)
		if secErr != nil {
			writeErr(w, http.StatusConflict, "нет сохранённого LLM-токена для перезапуска места")
			return
		}
		apiKey, decErr := s.sec.Decrypt(sec.Ciphertext)
		if decErr != nil || apiKey == "" {
			writeErr(w, http.StatusConflict, "не удалось получить LLM-токен для перезапуска места")
			return
		}
		models := ws.Models
		if len(models) == 0 && ws.Model != "" {
			models = []string{ws.Model}
		}
		provider = provision.Provider{BaseURL: ws.BaseURL, BaseURLs: ws.BaseURLs, Model: ws.Model, Models: models, APIKey: apiKey}
	}
	if err := s.db.UpdateWorkspaceSSH(ws.ID, key, port); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.db.Audit(id.Login, "workspace.ssh", ws.ID, fmt.Sprintf("key=%d bytes port=%d", len(key), port))
	if ws.Status == "running" {
		paths := s.pathsFor(ws, s.db.WorkspaceOwnerLogin(ws.OwnerID))
		_ = s.db.UpdateWorkspaceStatus(ws.ID, "provisioning", "применяем SSH-настройки и пересоздаём контейнер…")
		go s.recreateForSSH(ws.ID, paths, provider, id.Login)
		writeJSON(w, http.StatusAccepted, map[string]any{"sshPort": port, "enabled": key != "", "status": "provisioning"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sshPort": port, "enabled": key != ""})
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
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "не ваш воркспейс")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	var req ProviderReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	// Пустые поля = оставить текущие значения воркспейса.
	baseURLs := ws.BaseURLs
	if len(baseURLs) == 0 && ws.BaseURL != "" {
		baseURLs = []string{ws.BaseURL}
	}
	if requested := req.allBaseURLs(); len(requested) > 0 {
		if err := validateBaseURLs(requested); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		baseURLs = requested
	}
	if err := validateBaseURLs(baseURLs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	base := baseURLs[0]
	models := req.allModels()
	if len(models) == 0 {
		models = ws.Models
		if len(models) == 0 && ws.Model != "" {
			models = []string{ws.Model}
		}
	}
	if len(models) == 0 {
		writeErr(w, http.StatusBadRequest, "укажите хотя бы одну модель")
		return
	}
	key := ""
	if req.APIKey != "" {
		key = req.APIKey
	} else if sec, err := s.db.SecretByUser(ws.OwnerID); err == nil {
		if k, err := s.sec.Decrypt(sec.Ciphertext); err == nil {
			key = k
		}
	}
	if key == "" {
		writeErr(w, http.StatusBadRequest, "нет сохранённого токена — укажите API-токен")
		return
	}
	p := provision.Provider{BaseURL: base, BaseURLs: baseURLs, Model: models[0], Models: models, APIKey: key}
	paths := s.pathsFor(ws, s.db.WorkspaceOwnerLogin(ws.OwnerID))
	if err := provision.RefreshProvider(paths.DshHome, p); err != nil {
		writeErr(w, http.StatusInternalServerError, "настройки не записаны: "+err.Error())
		return
	}
	if err := s.db.UpdateWorkspaceProvider(ws.ID, base, baseURLs, models[0], models); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.APIKey != "" {
		if err := s.saveSecret(ws.OwnerID, p); err != nil {
			writeErr(w, http.StatusInternalServerError, "секрет не сохранён: "+err.Error())
			return
		}
	}
	_ = s.db.Audit(id.Login, "workspace.provider", ws.ID, "baseURLs="+strconv.Itoa(len(baseURLs))+" models="+strings.Join(models, ","))
	if ws.Status == "running" {
		if err := s.db.UpdateWorkspaceStatus(ws.ID, "provisioning", "применяем новые настройки провайдера…"); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		go s.recreateForProvider(ws.ID, paths, p, id.Login)
	}
	ws, _ = s.db.WorkspaceByID(ws.ID)
	writeJSON(w, http.StatusOK, map[string]any{"workspace": s.wsJSON(r.Context(), ws, id.Login)})
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
	if !s.cfg.RequireApproval {
		writeErr(w, http.StatusNotImplemented, "апрув-режим выключен: создавайте места напрямую")
		return
	}
	var req struct {
		Name       string   `json:"name"`
		Kit        string   `json:"kit"`
		Template   string   `json:"template"`
		Components []string `json:"components"`
		ProviderReq
		SSHPubKey string `json:"sshPubKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Kit == "" {
		req.Kit = req.Template
	}
	if len(req.Name) < 2 {
		writeErr(w, http.StatusBadRequest, "имя воркспейса: минимум 2 символа")
		return
	}
	kitID, components, err := provision.NormalizeComponents(req.Kit, req.Components)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.ProviderReq.validate(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.providerOf(store.Workspace{}, id.ID, req.ProviderReq); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sshPubKey, err := parseSSHPublicKey(req.SSHPubKey)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	own, err := s.db.WorkspacesByOwner(id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(own) >= s.cfg.MaxPerUser {
		writeErr(w, http.StatusBadRequest, "достигнут лимит воркспейсов ("+strconv.Itoa(s.cfg.MaxPerUser)+")")
		return
	}
	pending, err := s.db.WorkspaceRequestsByUser(id.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	pendingCount := 0
	for _, request := range pending {
		if request.Status == "pending" {
			pendingCount++
		}
	}
	if pendingCount >= s.cfg.MaxPerUser {
		writeErr(w, http.StatusBadRequest, "достигнут лимит ожидающих заявок ("+strconv.Itoa(s.cfg.MaxPerUser)+")")
		return
	}
	// дубликаты имён не проверяем заранее: при approve имя могло освободиться;
	// конфликт всплывёт там с понятной ошибкой, заявка вернётся в pending.

	payloadPlain := map[string]any{
		"baseURL": firstModel(req.allBaseURLs()), "baseURLs": req.allBaseURLs(),
		"model": firstModel(req.allModels()), "models": req.allModels(),
		"apiKey": req.APIKey,
	}
	blob, err := json.Marshal(payloadPlain)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось сериализовать заявку: "+err.Error())
		return
	}
	cipher, err := s.sec.Encrypt(string(blob))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось зашифровать заявку: "+err.Error())
		return
	}
	reqID, err := provision.NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	nowS := time.Now().UTC().Format("2006-01-02 15:04:05")
	wr := store.WorkspaceRequest{
		ID: reqID, UserID: id.ID, Name: req.Name, Kit: kitID, Components: components,
		Payload: cipher, SSHPubKey: sshPubKey, Status: "pending", CreatedAt: nowS, UpdatedAt: nowS,
	}
	if err := s.db.InsertWorkspaceRequest(wr); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "workspace.request", reqID, "name="+req.Name)
	writeJSON(w, http.StatusAccepted, map[string]any{"request": wsReqJSON(wr)})
}

// handleWorkspaceRequestsList — GET /api/workspace-requests:
// пользователь видит свои, админ — все.
func (s *Server) handleWorkspaceRequestsList(w http.ResponseWriter, r *http.Request, id identity) {
	var list []store.WorkspaceRequest
	var err error
	if id.Role == "admin" {
		list, err = s.db.WorkspaceRequestsAll()
	} else {
		list, err = s.db.WorkspaceRequestsByUser(id.ID)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]wsRequestOut, 0, len(list))
	for _, r := range list {
		out = append(out, wsReqJSON(r))
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

// handleWorkspaceRequestDecision — POST /api/workspace-requests/{id}/approve|reject (админ).
func (s *Server) handleWorkspaceRequestDecision(approve bool) func(http.ResponseWriter, *http.Request, identity) {
	return func(w http.ResponseWriter, r *http.Request, id identity) {
		if id.Role != "admin" {
			writeErr(w, http.StatusForbidden, "только администратор")
			return
		}
		s.requestMu.Lock()
		defer s.requestMu.Unlock()
		reqID := r.PathValue("id")
		wr, err := s.db.WorkspaceRequestByID(reqID)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "заявка не найдена")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if wr.Status != "pending" {
			writeErr(w, http.StatusConflict, "заявка уже обработана: "+wr.Status)
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		if !approve {
			if err := s.db.UpdateWorkspaceRequestStatus(reqID, "rejected", id.Login, strings.TrimSpace(body.Reason), ""); err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			_ = s.db.Audit(id.Login, "workspace.request.reject", reqID, body.Reason)
			writeJSON(w, http.StatusOK, map[string]any{"status": "rejected"})
			return
		}

		// Перед approve повторно проверяем квоту: пользователь мог создать
		// другие места, пока заявка ждала решения.
		own, err := s.db.WorkspacesByOwner(wr.UserID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(own) >= s.cfg.MaxPerUser {
			writeErr(w, http.StatusConflict, "у владельца заявки уже достигнут лимит воркспейсов")
			return
		}

		// approve: расшифровываем провайдера и запускаем provision владельцу.
		plain, err := s.sec.Decrypt(wr.Payload)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "payload не расшифрован: "+err.Error())
			return
		}
		var provReq ProviderReq
		if err := json.Unmarshal([]byte(plain), &provReq); err != nil {
			writeErr(w, http.StatusInternalServerError, "payload повреждён: "+err.Error())
			return
		}
		if err := provReq.validate(); err != nil {
			writeErr(w, http.StatusConflict, "провайдер в заявке больше не валиден: "+err.Error())
			return
		}
		provider, err := s.providerOf(store.Workspace{}, wr.UserID, provReq)
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		wsID, err := provision.NewID()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		login := s.db.WorkspaceOwnerLogin(wr.UserID)
		if login == "" {
			writeErr(w, http.StatusConflict, "владелец заявки не найден")
			return
		}
		paths := provision.Paths(s.cfg.Home, login, wsID, wr.Name)
		if _, err := os.Stat(paths.Repo); err == nil {
			writeErr(w, http.StatusConflict,
				"репозиторий «"+wr.Name+"» уже есть на диске: выберите другое имя или удалите "+paths.Repo)
			return
		}
		port, err := dockerrt.FreePort()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "нет свободного порта: "+err.Error())
			return
		}
		ws := store.Workspace{
			ID: wsID, OwnerID: wr.UserID, Name: wr.Name, Template: wr.Kit, Components: wr.Components,
			Status: "provisioning", HostPort: port, BaseURL: provider.BaseURL, BaseURLs: provider.AllBaseURLs(),
			Model: provider.DefaultModel(), Models: provider.AllModels(),
			SSHPubKey: wr.SSHPubKey,
		}
		sshPort := 0
		if ws.SSHPubKey != "" {
			if sshPort, err = dockerrt.FreePort(); err != nil {
				writeErr(w, http.StatusInternalServerError, "нет свободного SSH-порта: "+err.Error())
				return
			}
			ws.SSHPort = sshPort
		}
		if err := s.db.InsertWorkspace(ws); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.saveSecret(wr.UserID, provider); err != nil {
			_ = s.db.UpdateWorkspaceStatus(wsID, "failed", "секрет не сохранён: "+err.Error())
			writeErr(w, http.StatusInternalServerError, "секрет не сохранён: "+err.Error())
			return
		}
		if err := s.db.UpdateWorkspaceRequestStatus(reqID, "approved", id.Login, "", wsID); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		wr.Status, wr.DecidedBy, wr.WorkspaceID, wr.Login = "approved", id.Login, wsID, login
		_ = s.db.Audit(id.Login, "workspace.request.approve", reqID, "name="+wr.Name+" user="+login)
		go s.provisionJob(ws, login, provider)
		wsFresh, _ := s.db.WorkspaceByID(wsID)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"request": wsReqJSON(wr), "workspace": s.wsJSON(r.Context(), wsFresh, login),
		})
	}
}
