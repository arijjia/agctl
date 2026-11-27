package server

// local.go — однопользовательский локальный режим (agctl dev up): владелец
// машины — единственная учётка (роль developer), вход по одноразовому токену
// из CLI, регистрации нет; /workspace контейнера — родная папка проектов;
// мост к хосту (белый список команд) и ручки управления для CLI на хосте.

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/localbridge"
	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// LocalMode — настройки локального режима (Config.Local; nil = серверный режим).
type LocalMode struct {
	OwnerLogin   string             // имя владельца (не учётка: строки в users нет, роль developer)
	WorkDir      string             // абсолютная папка проектов → /workspace
	LoginToken   string             // токен входа /local/login (живёт до рестарта процесса)
	ControlToken string             // токен CLI на хосте: status/shutdown/approve
	BridgeToken  string             // токен контейнера к мосту
	BridgeURL    string             // адрес моста, видимый из контейнера
	BridgeTools  []localbridge.Tool // белый список хост-команд
	BridgeLog    string             // журнал вызовов моста
	Env          []string           // KEY=VALUE в контейнер (секреты из хранилища ОС)
	Shutdown     func()             // остановка процесса (POST /api/local/shutdown)
	// Hooks — хост для лаунчера: доступы (хранилище ОС), образы (config.yaml,
	// pull/сборка). nil — лаунчер без раздела локального режима, env из Env.
	Hooks LocalHooks
}

// localState — рантайм-состояние локального режима сервера.
type localState struct {
	ownerID int64
	bridge  *localbridge.Bridge
	task    *localTask // подготовка рабочего места из лаунчера

	workMu  sync.Mutex
	workDir string // папка проектов на хосте → /workspace (меняется из лаунчера)

	// Лаунчер v2: задачи подготовки рабочих мест (по слотам; слот 0 —
	// task выше), поток событий для открытых копий интерфейса, ревизия
	// основного места.
	placeMu    sync.Mutex
	placeTasks map[int]*localTask
	imageTask  *localTask // операция раздела «Образы» (local_images.go)
	events     *localEvents
	mainRev    int64

	// profileExtras — доп. провайдеры профиля (снимок на старте); личные
	// провайдеры разработчика добавляются поверх (local_providers.go).
	profileExtras []provision.ExtraProvider
}

// mainRevision — ревизия основного рабочего места (папка и образ из config.yaml).
func (l *localState) mainRevision() int64 {
	l.placeMu.Lock()
	defer l.placeMu.Unlock()
	return l.mainRev
}

// bumpMain — основное место изменилось (папка или образ).
func (l *localState) bumpMain() {
	l.placeMu.Lock()
	l.mainRev++
	l.placeMu.Unlock()
}

// localWorkDir — текущая папка проектов владельца.
func (s *Server) localWorkDir() string {
	if s.local == nil {
		return s.cfg.Local.WorkDir
	}
	s.local.workMu.Lock()
	defer s.local.workMu.Unlock()
	return s.local.workDir
}

// setLocalWorkDir — сменить папку проектов: work-корень владельца и маунт
// /workspace следующего старта рабочего места.
func (s *Server) setLocalWorkDir(dir string) {
	s.local.workMu.Lock()
	s.local.workDir = dir
	s.local.workMu.Unlock()
	provision.SetLocalWorkRoot(s.cfg.Home, s.local.ownerID, dir)
}

func (s *Server) isLocal() bool { return s.cfg.Local != nil }

// initLocal — владелец (без учётки, local_owner.go), подмена work-корня, мост.
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
	id := s.localOwnerIDFor(lm.OwnerLogin)
	s.useOwnerSessions(id, lm.OwnerLogin)
	b, err := localbridge.New(lm.BridgeTools, lm.BridgeLog)
	if err != nil {
		return err
	}
	s.local = &localState{ownerID: id, bridge: b, task: &localTask{state: "idle"}, workDir: lm.WorkDir,
		events: &localEvents{}, mainRev: 1}
	s.local.profileExtras = append([]provision.ExtraProvider(nil), s.cfg.DefaultLLM.Extras...)
	s.syncLocalProviders()
	provision.SetLocalWorkRoot(s.cfg.Home, id, lm.WorkDir)
	// Источник правды об образе — config.yaml (его меняют и лаунчер, и CLI).
	if lm.Hooks != nil {
		if err := s.db.SettingSet(chatImageSettingKey(id, 0), lm.Hooks.CurrentImage()); err != nil {
			return err
		}
	}
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
	s.registerLocalUIRoutes(mux)
	s.registerLocalPlaceRoutes(mux)
	s.registerLocalImageRoutes(mux)
	s.registerLocalPresetRoutes(mux)
	s.registerLocalProviderRoutes(mux)
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
	if h := s.cfg.Local.Hooks; h != nil {
		// Лаунчер: образ и доступы — в браузере. Если всё готово (образ на
		// машине, ключ модели задан) — рабочее место прогревается сразу.
		ready := h.LLMKey() != ""
		cur := h.CurrentImage()
		present := false
		for _, im := range h.Images() {
			present = present || (im.Ref == cur && im.Present)
		}
		if ready && present {
			s.startLocalRuntime(false)
		}
		http.Redirect(w, r, "/launcher", http.StatusSeeOther)
		return
	}
	go func() {
		if _, err := s.ensureChat(s.local.ownerID, 0); err != nil {
			log.Printf("[local] старт рабочего места: %v", err)
		}
	}()
	http.Redirect(w, r, "/chat/", http.StatusSeeOther)
}

func (s *Server) handleLocalStatus(w http.ResponseWriter, r *http.Request) {
	s.chatMu.Lock()
	rt := s.chats[chatKey{uid: s.local.ownerID, slot: 0}]
	s.chatMu.Unlock()
	runtime := map[string]any{"state": "sleeping"}
	if rt != nil {
		runtime = map[string]any{"state": "running", "image": rt.image, "endpoint": rt.handle.Endpoint, "kind": handleKind(rt.handle)}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.cfg.Version, "image": s.cfg.Image, "workdir": s.localWorkDir(),
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

// localExtras — дополнения спецификации рабочего места в локальном режиме.
type localExtras struct {
	WorkDir string          // одна папка → /workspace
	Mounts  []agctlrt.Mount // несколько папок → /workspace/<имя>
	Env     []string        // доступы из хранилища ОС, адрес и токен моста
	DshHome string          // явная папка dsh-home места ("" — в доме agctl)
	// HostPaths — «пути как на хосте» (config.yaml host_paths, рантайм docker).
	HostPaths bool
	// CAFile, CAFull — набор корневых сертификатов для контейнера (certs.go
	// локального режима): путь на хосте и полный ли он.
	CAFile string
	CAFull bool
}

// LocalCAHooks — набор сертификатов организации для рабочих мест: хост
// собирает его на каждом старте места ("" — не настроен).
type LocalCAHooks interface {
	CABundle() (path string, full bool, err error)
}

// localChatExtras дополняет спецификацию рабочего места владельца в локальном
// режиме: папки хоста (слот 0 — папка проектов из config.yaml, остальные —
// из таблицы places), секреты из хранилища ОС, адрес моста. ok=false — не
// локальный режим или не владелец; ошибка — у слота нет рабочего места.
func (s *Server) localChatExtras(key chatKey) (localExtras, bool, error) {
	if !s.isLocal() || s.local == nil || key.uid != s.local.ownerID {
		return localExtras{}, false, nil
	}
	var ex localExtras
	if key.slot == 0 {
		ex.WorkDir = s.localWorkDir()
	} else {
		p, err := s.localPlaceBySlot(key)
		if err != nil {
			return localExtras{}, false, err
		}
		ex.WorkDir, ex.Mounts = placeBinds(p)
	}
	ex.DshHome = s.customDshHome(key)
	ex.HostPaths = s.localHostPaths()
	if h, ok := s.localHooks().(LocalCAHooks); ok {
		path, full, err := h.CABundle()
		if err != nil {
			log.Printf("[local] сертификаты для рабочего места: %v — место стартует без них", err)
		}
		ex.CAFile, ex.CAFull = path, full
	}
	var env []string
	if h := s.cfg.Local.Hooks; h != nil {
		env = append(env, h.ContainerEnv()...)
	} else {
		env = append(env, s.cfg.Local.Env...)
	}
	env = append(env,
		localbridge.EnvURL+"="+s.cfg.Local.BridgeURL,
		localbridge.EnvToken+"="+s.cfg.Local.BridgeToken,
		// Плагины dsh узнают локальный режим: реестр воркспейсов сверяется
		// с содержимым /workspace (папка проектов могла смениться).
		"AGCTL_LOCAL_MODE=1",
	)
	ex.Env = env
	return ex, true, nil
}

// writeLocalBridgeSkill кладёт скилл моста в dsh-home владельца (dsh
// подхватывает <dshHome>/skills без рестарта).
func (s *Server) writeLocalBridgeSkill(key chatKey) {
	if !s.isLocal() || s.local == nil || key.uid != s.local.ownerID {
		return
	}
	dir := filepath.Join(s.dshHomeFor(key), "skills", "agctl-host-bridge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[local] скилл моста: %v", err)
		return
	}
	body := localbridge.SkillMarkdown(s.local.bridge.Tools())
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		log.Printf("[local] скилл моста: %v", err)
	}
}
