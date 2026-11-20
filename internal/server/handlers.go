package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/profile"
	"golang.org/x/crypto/ssh"
)

// ── регистрация / вход ──────────────────────────────────────────────────────

type credsReq struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// parseSSHPublicKey валидирует и нормализует публичный SSH-ключ:
// пустая строка легальна (sshd выключен), длина ограничена 4 KiB,
// хвостовых данных после ключа быть не должно.
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

// validate проверяет минимальные требования к логину (≥3 символов) и
// паролю (≥6 символов) до похода в базу.
func (c credsReq) validate() error {
	if len(strings.TrimSpace(c.Login)) < 3 {
		return errors.New("логин: минимум 3 символа")
	}
	if len(c.Password) < 6 {
		return errors.New("пароль: минимум 6 символов")
	}
	return nil
}

// requestIP — адрес клиента для лимита регистраций: за доверенным
// ingress берём первый hop X-Forwarded-For, иначе RemoteAddr без порта.
func requestIP(r *http.Request) string {
	if ff := r.Header.Get("X-Forwarded-For"); ff != "" {
		if first := strings.TrimSpace(strings.Split(ff, ",")[0]); first != "" {
			return first
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// handleRegister — POST /api/register: создание учётки; первый пользователь
// платформы становится admin, остальные user. Пароль хешируется argon2id,
// факт регистрации и роль пишутся в audit, сессия ставится сразу.
// Частота регистраций с одного адреса ограничена (дефект D3).
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.regLimit != nil {
		if ok, retryAfter := s.regLimit.Allow(requestIP(r)); !ok {
			secs := int((retryAfter + time.Second - 1) / time.Second)
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			writeErr(w, http.StatusTooManyRequests,
				"слишком много регистраций с этого адреса — повторите через "+strconv.Itoa(secs)+" с")
			return
		}
	}
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

// handleLogin — POST /api/login: rate-limit по логину, сверка пароля
// (constant-time), постановка cookie-сессии; неудачи фиксируются в rate-limit,
// успешный вход сбрасывает счётчик и пишется в audit.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req credsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	if lock := s.authN.LockedFor(req.Login); lock > 0 {
		// Дефект D1: при блокировке клиент должен понимать, когда она снимется.
		secs := int((lock + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeErr(w, http.StatusTooManyRequests, fmt.Sprintf(
			"слишком много попыток — вход заблокирован, осталось %d мин %d с", secs/60, secs%60))
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
	// Prewarm-пул: тёплая среда к следующему заходу в /chat/ (карточка
	// chat-prewarm-pool). Асинхронно — логин не ждёт docker.
	if s.prewarmEnabled() {
		go s.prewarmOnce([]int64{u.ID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": map[string]string{"login": u.Login, "role": u.Role}})
}

// handleLogout — POST /api/logout: удаление сессии и сброс cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.authN.Logout(w, r)
	writeJSON(w, http.StatusOK, map[string]any{})
}

// handleMe — GET /api/me: текущий пользователь (login, role) по cookie.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, err := s.who(r)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "требуется вход")
		return
	}
	// sshEnabled=false — локальный режим: SSH в воркспейсах не публикуется
	// (ключ не выдаётся, порт 2222 закрыт) — кабинет dsh прячет SSH-вкладку.
	// localMode — плагины dsh прячут серверное (файлы и «Скачать»).
	writeJSON(w, http.StatusOK, map[string]any{
		"user":       map[string]string{"login": id.Login, "role": id.Role},
		"sshEnabled": s.local == nil,
		"localMode":  s.local != nil,
	})
}

// pathUnder — путь p лежит внутри каталога home (после нормализации).
// pathUnder — путь p лежит внутри каталога home (после нормализации); защита
// от выхода за пределы дома через `..`.
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

// встроенные ассеты для provision (сырые FS; префиксы знает provision).
var assetsDshSkel = assetsSkel // префикс dsh-skel

// redactWorkspaceLogs — runtime boot-логи могут включать dsh login token и
// случайно напечатанные credentials; UI/API отдаёт только санитизированный
// хвост (query-токены, Bearer, AGCTL_LLM_API_KEY, dsh-auth cookie, sk-ключи).
func redactWorkspaceLogs(logs string) string {
	logs = workspaceLogQueryTokenRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogBearerRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogEnvKeyRE.ReplaceAllString(logs, `${1}[redacted]`)
	logs = workspaceLogCookieRE.ReplaceAllString(logs, `${1}[redacted]`)
	return workspaceLogAPIKeyRE.ReplaceAllString(logs, "[redacted]")
}

var (
	workspaceLogQueryTokenRE = regexp.MustCompile(`(?i)([?&]token=)[^&\s"'<>]+`)
	workspaceLogBearerRE     = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/-]+=*`)
	workspaceLogEnvKeyRE     = regexp.MustCompile(`(?i)(AGCTL_LLM_API_KEY\s*=\s*)[^\s"'<>]+`)
	workspaceLogCookieRE     = regexp.MustCompile(`(?i)(dsh-auth-[A-Za-z0-9_-]+=)[^;\s]+`)
	workspaceLogAPIKeyRE     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
)

// ── админка ─────────────────────────────────────────────────────────────────

// handleAdminUsers — GET /api/admin/users: список учётных записей платформы.
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

// handleAdminAudit — GET /api/admin/audit: свежие события журнала безопасности.
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

// handleConfig — публичные для авторизованного клиента флаги платформы
// (панель Кабинета подстраивает форму создания места).
// handleConfig — GET /api/config: публичные для клиента флаги платформы
// (панель Кабинета подстраивает форму создания места).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tier0": s.cfg.Tier0,
		// Готовые сценарии рабочего места — из профиля организации
		// (workspace.scenarios): клиент показывает их кнопками в диалоге
		// создания воркспейса. Нейтральный профиль сценариев не несёт.
		"presets": scenarioPresets(s.brand.Scenarios),
	})
}

// scenarioPresets — сценарии профиля для JSON (пустой список, не null).
func scenarioPresets(in []profile.Scenario) []profile.Scenario {
	if in == nil {
		return []profile.Scenario{}
	}
	return in
}
