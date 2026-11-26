package server

// local_ui.go — лаунчер разработчика в локальном режиме (agctl dev): после
// входа владелец попадает на /launcher, где выбирает образ рабочего места
// (с отметкой «есть на этой машине»; недостающий скачивается или собирается
// здесь же) и управляет доступами — ключ модели, GitLab, Jira. Значения
// секретов в браузер не возвращаются никогда, только «задан / не задан».
// Всё, что касается хоста (хранилище ОС, config.yaml, docker pull/build), —
// за интерфейсом LocalHooks, его реализует cmd/agctl.

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/localmode"
)

// LocalImage — образ для выбора в лаунчере.
type LocalImage struct {
	Ref     string `json:"ref"`
	Present bool   `json:"present"` // есть в локальном Docker
	Flavor  string `json:"flavor,omitempty"`
	// Что внутри (метки io.agctl.* образа; у собранных до меток — пусто).
	DshVersion    string `json:"dshVersion,omitempty"`
	GuestRevision string `json:"guestRevision,omitempty"`
	GuestAPI      int    `json:"guestApi,omitempty"`
	Variant       string `json:"variant,omitempty"` // full | min | web | …
	Built         string `json:"built,omitempty"`   // дата сборки
	SizeMB        int64  `json:"sizeMb,omitempty"`
	// Outdated — образ старее этой сборки agctl (уровень API guest ниже
	// нужного или меток нет): часть возможностей лаунчера в нём не работает.
	Outdated bool   `json:"outdated,omitempty"`
	Note     string `json:"note,omitempty"`
}

// LocalHooks — операции на хосте для лаунчера локального режима.
type LocalHooks interface {
	Entries() []localmode.Entry
	SetValue(name, value string, plain bool) error
	DeleteValue(name string) error
	ContainerEnv() []string // env рабочего места (без ключа модели)
	LLMKey() string
	StoreBackend() string
	Images() []LocalImage
	CurrentImage() string
	SetImage(ref string) error
	// EnsureImage: docker pull, а нет в реестре — сборка цепочки на машине.
	EnsureImage(ctx context.Context, ref string, logf func(string)) error
	// SetWorkDir — папка проектов на хосте (config.yaml + файл состояния);
	// возвращает проверенный абсолютный путь.
	SetWorkDir(dir string) (string, error)
}

// localTask — фоновая подготовка рабочего места (скачивание/сборка образа и
// старт контейнера), видна в лаунчере.
type localTask struct {
	mu      sync.Mutex
	state   string // idle | building | starting | error
	image   string
	log     []string
	err     string
	pending bool // доступы/образ изменены, рабочее место ещё на старых
}

func (t *localTask) snapshot() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	lines := append([]string{}, t.log...)
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return map[string]any{"state": t.state, "image": t.image, "log": lines, "error": t.err, "restartNeeded": t.pending}
}

func (t *localTask) logf(m string) {
	t.mu.Lock()
	t.log = append(t.log, time.Now().Format("15:04:05")+"  "+m)
	t.mu.Unlock()
	log.Printf("[local] %s", m)
}

func (t *localTask) busy() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.state == "building" || t.state == "starting"
}

func (t *localTask) markPending() {
	t.mu.Lock()
	t.pending = true
	t.mu.Unlock()
}

func (s *Server) localHooks() LocalHooks {
	if !s.isLocal() || s.local == nil {
		return nil
	}
	return s.cfg.Local.Hooks
}

func (s *Server) registerLocalUIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/local/ui", s.requireLocalOwner(s.handleLocalUI))
	mux.HandleFunc("PUT /api/local/ui/secrets/{name}", s.requireLocalOwner(s.handleLocalSecretPut))
	mux.HandleFunc("DELETE /api/local/ui/secrets/{name}", s.requireLocalOwner(s.handleLocalSecretDelete))
	mux.HandleFunc("PUT /api/local/ui/image", s.requireLocalOwner(s.handleLocalImagePut))
	mux.HandleFunc("PUT /api/local/ui/workdir", s.requireLocalOwner(s.handleLocalWorkDirPut))
	mux.HandleFunc("PUT /api/local/ui/runtime", s.requireLocalOwner(s.handleLocalRuntimePut))
	mux.HandleFunc("POST /api/local/ui/start", s.requireLocalOwner(s.handleLocalStart))
	mux.HandleFunc("POST /api/local/ui/restart", s.requireLocalOwner(s.handleLocalRestart))
}

// requireLocalOwner — сессия владельца локального режима + same-origin для
// любых изменений (cookie и так SameSite=Lax; Origin — второй рубеж).
func (s *Server) requireLocalOwner(next func(http.ResponseWriter, *http.Request, identity)) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, id identity) {
		if s.localHooks() == nil || id.ID != s.local.ownerID {
			writeErr(w, http.StatusNotFound, "доступно только в локальном режиме (agctl dev)")
			return
		}
		if r.Method != http.MethodGet {
			if o := r.Header.Get("Origin"); o != "" && !sameOriginAuthority(o, r.Host) {
				writeErr(w, http.StatusForbidden, "чужой Origin")
				return
			}
		}
		next(w, r, id)
	})
}

func (s *Server) handleLocalUI(w http.ResponseWriter, r *http.Request, id identity) {
	h := s.localHooks()
	s.chatMu.Lock()
	rt := s.chats[chatKey{uid: id.ID, slot: 0}]
	s.chatMu.Unlock()
	runtime := map[string]any{"state": "sleeping"}
	if rt != nil {
		runtime = map[string]any{"state": "running", "image": rt.image, "kind": handleKind(rt.handle)}
	}
	resp := map[string]any{
		"profile":   s.brand.Name,
		"workdir":   s.localWorkDir(),
		"images":    h.Images(),
		"current":   h.CurrentImage(),
		"secrets":   h.Entries(),
		"storage":   h.StoreBackend(),
		"runtime":   runtime,
		"task":      s.local.task.snapshot(),
		"llmKeySet": h.LLMKey() != "",
	}
	// runtimeKind — выбранный рантайм (docker | sbx), runtimes — что есть на
	// машине; runtime.kind — в каком поднято работающее место.
	resp["runtimeKind"], resp["runtimes"] = s.localRuntimes()
	writeJSON(w, http.StatusOK, resp)
}

type localSecretReq struct {
	Value string `json:"value"`
	Plain bool   `json:"plain"`
}

func (s *Server) handleLocalSecretPut(w http.ResponseWriter, r *http.Request, id identity) {
	var req localSecretReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	name := r.PathValue("name")
	if err := s.localHooks().SetValue(name, req.Value, req.Plain); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.afterLocalSecretChange(name, req.Value)
	_ = s.db.Audit(id.Login, "local.secret.set", name, "")
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "set": true})
}

func (s *Server) handleLocalSecretDelete(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("name")
	if err := s.localHooks().DeleteValue(name); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.afterLocalSecretChange(name, "")
	_ = s.db.Audit(id.Login, "local.secret.delete", name, "")
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "set": false})
}

// afterLocalSecretChange — ключ модели и личные ключи доп. провайдеров
// сразу в провайдер сервера; всё остальное — env контейнера: нужно
// перезапустить рабочее место.
func (s *Server) afterLocalSecretChange(name, value string) {
	if name == "AGCTL_LLM_API_KEY" {
		s.setDefaultLLMKey(s.localHooks().LLMKey())
	} else {
		s.setDefaultLLMExtraKey(name, value)
	}
	if localmode.IsModelKeyEnv(name) {
		// Ключ модели харнесс подхватывает сразу: .credentials.yaml он
		// перечитывает сам, перезапуск места не нужен.
		s.syncLocalProviders()
		s.pushLLMToPlaces()
		s.local.events.publish("secrets")
		return
	}
	// Доступы общие для всех рабочих мест: каждое работающее — на старых.
	s.markLocalPlacesPending()
	s.local.events.publish("secrets")
	s.local.events.publish("places")
}

type localImageReq struct {
	Image string `json:"image"`
}

func (s *Server) handleLocalImagePut(w http.ResponseWriter, r *http.Request, id identity) {
	var req localImageReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	ref := strings.TrimSpace(req.Image)
	ok := false
	for _, im := range s.localHooks().Images() {
		ok = ok || im.Ref == ref
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "образ не из каталога профиля")
		return
	}
	if err := s.localHooks().SetImage(ref); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.SettingSet(chatImageSettingKey(id.ID, 0), ref); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.chatMu.Lock()
	rt := s.chats[chatKey{uid: id.ID, slot: 0}]
	s.chatMu.Unlock()
	if rt != nil && rt.image != ref {
		s.local.task.markPending()
	}
	s.local.bumpMain()
	s.local.events.publish("places")
	_ = s.db.Audit(id.Login, "local.image.set", ref, "")
	writeJSON(w, http.StatusOK, map[string]any{"image": ref})
}

type localWorkDirReq struct {
	WorkDir string `json:"workdir"`
}

// handleLocalWorkDirPut — PUT /api/local/ui/workdir: папка проектов на хосте
// (→ /workspace). Применяется при следующем старте рабочего места; работающее
// место помечается «нужен перезапуск».
func (s *Server) handleLocalWorkDirPut(w http.ResponseWriter, r *http.Request, id identity) {
	var req localWorkDirReq
	if err := decodeJSONBody(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	abs, err := s.localHooks().SetWorkDir(strings.TrimSpace(req.WorkDir))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prev := s.localWorkDir()
	s.setLocalWorkDir(abs)
	if abs != prev {
		s.chatMu.Lock()
		running := s.chats[chatKey{uid: id.ID, slot: 0}] != nil
		s.chatMu.Unlock()
		if running {
			s.local.task.markPending()
		}
	}
	s.local.bumpMain()
	s.local.events.publish("places")
	_ = s.db.Audit(id.Login, "local.workdir.set", abs, "")
	writeJSON(w, http.StatusOK, map[string]any{"workdir": abs})
}

func (s *Server) handleLocalStart(w http.ResponseWriter, r *http.Request, id identity) {
	if !s.startLocalRuntime(false) {
		writeErr(w, http.StatusConflict, "подготовка уже идёт")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "starting"})
}

func (s *Server) handleLocalRestart(w http.ResponseWriter, r *http.Request, id identity) {
	if !s.startLocalRuntime(true) {
		writeErr(w, http.StatusConflict, "подготовка уже идёт")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "restarting"})
}

// startLocalRuntime — в фоне: (остановить старое рабочее место) → образ
// есть? иначе скачать/собрать → старт контейнера. false — уже идёт.
func (s *Server) startLocalRuntime(restart bool) bool {
	t := s.local.task
	t.mu.Lock()
	if t.state == "building" || t.state == "starting" {
		t.mu.Unlock()
		return false
	}
	ref := s.localHooks().CurrentImage()
	t.state, t.image, t.err, t.log = "starting", ref, "", nil
	t.mu.Unlock()
	go func() {
		defer s.local.events.publish("places")
		owner := s.local.ownerID
		fail := func(err error) {
			t.mu.Lock()
			t.state, t.err = "error", err.Error()
			t.mu.Unlock()
			log.Printf("[local] рабочее место: %v", err)
		}
		if restart {
			s.chatMu.Lock()
			rt := s.chats[chatKey{uid: owner, slot: 0}]
			s.chatMu.Unlock()
			if rt != nil {
				t.logf("останавливаю рабочее место")
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				err := s.chatProvider.StopChat(ctx, rt.handle)
				cancel()
				if err != nil {
					fail(err)
					return
				}
				s.evictChatByUser(owner, 0)
			}
		}
		present := false
		for _, im := range s.localHooks().Images() {
			present = present || (im.Ref == ref && im.Present)
		}
		if !present {
			t.mu.Lock()
			t.state = "building"
			t.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
			err := s.localHooks().EnsureImage(ctx, ref, t.logf)
			cancel()
			if err != nil {
				fail(err)
				return
			}
			t.mu.Lock()
			t.state = "starting"
			t.mu.Unlock()
		}
		t.logf("запускаю рабочее место на " + ref)
		// Пресеты основного места кладутся в dsh-home до entrypoint (local_presets.go).
		if err := s.ensureChatWithPresets(owner, 0, t.logf); err != nil {
			fail(err)
			return
		}
		t.logf("рабочее место запущено")
		t.mu.Lock()
		t.state, t.pending = "idle", false
		t.mu.Unlock()
	}()
	return true
}
