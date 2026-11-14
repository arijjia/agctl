package server

// local.go — однопользовательский локальный режим (agctl dev up): владелец
// машины — единственная учётка (роль developer), вход по одноразовому токену
// из CLI, регистрации нет; /workspace контейнера — родная папка проектов;
// мост к хосту (белый список команд) и ручки управления для CLI на хосте.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/auth"
	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/provision"
)

// LocalMode — настройки локального режима (Config.Local; nil = серверный режим).
type LocalMode struct {
	OwnerLogin   string             // учётка-владелец (developer), создаётся автоматически
	WorkDir      string             // абсолютная папка проектов → /workspace
	LoginToken   string             // токен входа /local/login (живёт до рестарта процесса)
	ControlToken string             // токен CLI на хосте: status/shutdown/approve
	BridgeToken  string             // токен контейнера к мосту
	BridgeURL    string             // адрес моста, видимый из контейнера
	BridgeTools  []localbridge.Tool // белый список хост-команд
	BridgeLog    string             // журнал вызовов моста
	Env          []string           // KEY=VALUE в контейнер (секреты из хранилища ОС)
	Shutdown     func()             // остановка процесса (POST /api/local/shutdown)
}

// localState — рантайм-состояние локального режима сервера.
type localState struct {
	ownerID int64
	bridge  *localbridge.Bridge
}

func (s *Server) isLocal() bool { return s.cfg.Local != nil }

// initLocal — учётка-владелец, подмена work-корня, мост.
func (s *Server) initLocal() error {
	lm := s.cfg.Local
	if lm.OwnerLogin == "" {
		lm.OwnerLogin = "owner"
	}
	if !filepath.IsAbs(lm.WorkDir) {
		return fmt.Errorf("local: папка проектов должна быть абсолютной: %q", lm.WorkDir)
	}
	if lm.LoginToken == "" || lm.ControlToken == "" || lm.BridgeToken == "" {
		return fmt.Errorf("local: токены входа/управления/моста обязательны")
	}
	u, err := s.db.UserByLogin(lm.OwnerLogin)
	var id int64
	if err != nil {
		// Пароль владельцу не нужен (вход — токеном из CLI), но поле
		// обязательное: случайный, никому не известный.
		pw := make([]byte, 24)
		_, _ = rand.Read(pw)
		hash, herr := auth.HashPassword(base64.RawURLEncoding.EncodeToString(pw))
		if herr != nil {
			return herr
		}
		if id, err = s.db.CreateUser(lm.OwnerLogin, hash, "developer"); err != nil {
			return fmt.Errorf("local: учётка владельца: %w", err)
		}
	} else {
		id = u.ID
		if u.Role != "developer" {
			if err := s.db.UpdateUserRole(id, "developer"); err != nil {
				return err
			}
		}
	}
	b, err := localbridge.New(lm.BridgeTools, lm.BridgeLog)
	if err != nil {
		return err
	}
	s.local = &localState{ownerID: id, bridge: b}
	provision.SetLocalWorkRoot(s.cfg.Home, id, lm.WorkDir)
	return nil
}

func (s *Server) registerLocalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /local/login", s.handleLocalLogin)
	mux.HandleFunc("GET /api/local/status", s.requireControl(s.handleLocalStatus))
	mux.HandleFunc("POST /api/local/shutdown", s.requireControl(s.handleLocalShutdown))
	mux.HandleFunc("GET /api/local/bridge/pending", s.requireControl(s.handleBridgePending))
	mux.HandleFunc("POST /api/local/bridge/pending/{id}/{verdict}", s.requireControl(s.handleBridgeDecide))
	mux.HandleFunc("GET "+localbridge.PathTools, s.requireBridge(s.handleBridgeTools))
	mux.HandleFunc("POST "+localbridge.PathCall, s.requireBridge(s.handleBridgeCall))
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
}

func tokenEqual(got, want string) bool {
	return got != "" && want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) requireControl(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !tokenEqual(bearer(r), s.cfg.Local.ControlToken) {
			writeErr(w, http.StatusUnauthorized, "нужен токен управления локального режима")
			return
		}
		next(w, r)
	}
}

func (s *Server) requireBridge(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !tokenEqual(bearer(r), s.cfg.Local.BridgeToken) {
			writeErr(w, http.StatusUnauthorized, "нужен токен моста")
			return
		}
		next(w, r)
	}
}

// handleLocalLogin — GET /local/login?token=…: сессия владельца и переход в
// чат; рабочее место прогревается сразу (без кнопки «Запустить»).
func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	if !tokenEqual(r.URL.Query().Get("token"), s.cfg.Local.LoginToken) {
		writeErr(w, http.StatusUnauthorized, "ссылка входа устарела — выполните agctl dev open")
		return
	}
	if err := s.authN.Issue(w, s.local.ownerID); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	go func() {
		if _, err := s.ensureChat(s.local.ownerID); err != nil {
			log.Printf("[local] старт рабочего места: %v", err)
		}
	}()
	// Образ выбран в dev.yaml, рабочее место уже прогревается — сразу в чат
	// (лаунчер доступен по /launcher: логи, рестарт контейнера).
	http.Redirect(w, r, "/chat/", http.StatusSeeOther)
}

func (s *Server) handleLocalStatus(w http.ResponseWriter, r *http.Request) {
	s.chatMu.Lock()
	rt := s.chats[s.local.ownerID]
	s.chatMu.Unlock()
	runtime := map[string]any{"state": "sleeping"}
	if rt != nil {
		runtime = map[string]any{"state": "running", "image": rt.image, "endpoint": rt.handle.Endpoint}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.cfg.Version, "image": s.cfg.Image, "workdir": s.cfg.Local.WorkDir,
		"owner": s.cfg.Local.OwnerLogin, "runtime": runtime,
		"bridgeTools": len(s.local.bridge.Tools()), "bridgePending": len(s.local.bridge.PendingList()),
		"profile": s.brand.ID,
	})
}

func (s *Server) handleLocalShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "stopping"})
	if s.cfg.Local.Shutdown != nil {
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.cfg.Local.Shutdown()
		}()
	}
}

func (s *Server) handleBridgeTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.local.bridge.Tools()})
}

func (s *Server) handleBridgeCall(w http.ResponseWriter, r *http.Request) {
	var req localbridge.CallRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	res, err := s.local.bridge.Call(r.Context(), req.Tool, req.Args)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.db.Audit(s.cfg.Local.OwnerLogin, "local.bridge.call", req.Tool, fmt.Sprintf("exit=%d", res.ExitCode))
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleBridgePending(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"pending": s.local.bridge.PendingList()})
}

func (s *Server) handleBridgeDecide(w http.ResponseWriter, r *http.Request) {
	approve := false
	switch r.PathValue("verdict") {
	case "approve":
		approve = true
	case "deny":
	default:
		writeErr(w, http.StatusBadRequest, "verdict: approve | deny")
		return
	}
	if err := s.local.bridge.Decide(r.PathValue("id"), approve); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "approved": approve})
}

// localChatSpec дополняет спецификацию рабочего места владельца в локальном
// режиме: родная папка проектов, секреты из хранилища ОС, адрес моста.
func (s *Server) localChatExtras(userID int64) (workDir string, env []string) {
	if !s.isLocal() || s.local == nil || userID != s.local.ownerID {
		return "", nil
	}
	env = append(env, s.cfg.Local.Env...)
	env = append(env,
		localbridge.EnvURL+"="+s.cfg.Local.BridgeURL,
		localbridge.EnvToken+"="+s.cfg.Local.BridgeToken,
	)
	return s.cfg.Local.WorkDir, env
}

// writeLocalBridgeSkill кладёт скилл моста в dsh-home владельца (dsh
// подхватывает <dshHome>/skills без рестарта).
func (s *Server) writeLocalBridgeSkill(userID int64) {
	if !s.isLocal() || s.local == nil || userID != s.local.ownerID {
		return
	}
	dir := filepath.Join(s.projectDshHome(userID), "skills", "agctl-host-bridge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[local] скилл моста: %v", err)
		return
	}
	body := localbridge.SkillMarkdown(s.local.bridge.Tools())
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		log.Printf("[local] скилл моста: %v", err)
	}
}
