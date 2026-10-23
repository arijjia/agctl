package server

// chat.go — режим 1 «Чат»: lazy per-user runtime behind the agctl gateway.
// The server depends on runtime.ChatProvider, not Docker ports or host mounts;
// Docker is one adapter and a Kubernetes adapter can return Service DNS + PVC.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
	"github.com/arijjia/agctl/internal/store"
)

const chatNamePrefix = "chat"

// chatDownGrace — сколько endpoint рантайма может не отвечать (dial-ошибками),
// прежде чем кэш признается мёртвым. Меньший порог ломает нормальный прогрев:
// dsh в свежесозданном контейнере начинает слушать порт не мгновенно.
const chatDownGrace = 45 * time.Second

// chatRuntime — живой персональный чат runtime пользователя. downSince — время
// первого dial-отказа прокси (ноль = живой); лечит вечное «запускается…» после
// смерти контейнера вне жизненного цикла серва (рестарт Docker VM, ручной
// docker rm): кэш сбрасывается, и /chat/ лениво пересоздаёт рантайм.
type chatRuntime struct {
	handle    agctlrt.ChatHandle
	lastSeen  time.Time
	active    int // streaming HTTP/WS requests; reaper must not stop active sessions
	downSince time.Time
}

// shouldSleepChat — решение рипера: runtime простаивал дольше TTL.
func shouldSleepChat(lastSeen, now time.Time, ttl time.Duration) bool {
	return now.Sub(lastSeen) >= ttl
}

// chatRuntimeID namespaces resource names by agctl home, so deployments sharing
// a Docker daemon or Kubernetes namespace do not collide.
func (s *Server) chatRuntimeID(userID int64) string {
	sum := sha256.Sum256([]byte(filepath.Clean(s.cfg.Home)))
	return fmt.Sprintf("%s-%x-u%d", chatNamePrefix, sum[:4], userID)
}

func chatStorageID(userID int64) string { return "u" + strconv.FormatInt(userID, 10) }

const (
	chatFoldersStateMaxBytes = 256 << 10
	maxChatFoldersPerUser    = 64
	maxChatFolderAssignments = 5000
)

type chatFolderSetting struct {
	Instructions string `json:"instructions"`
}

type chatFolderPending struct {
	Name  string   `json:"name"`
	Known []string `json:"known"`
	At    int64    `json:"at"`
}

type chatFoldersState struct {
	Folders       []string                     `json:"folders"`
	Assign        map[string]string            `json:"assign"`
	Settings      map[string]chatFolderSetting `json:"settings"`
	Active        string                       `json:"active"`
	PendingFolder *chatFolderPending           `json:"pendingFolder,omitempty"`
}

func emptyChatFoldersState() chatFoldersState {
	return chatFoldersState{Folders: []string{}, Assign: map[string]string{}, Settings: map[string]chatFolderSetting{}}
}

func chatFoldersSettingKey(userID int64) string {
	return "chat-folders-u" + strconv.FormatInt(userID, 10)
}

// handleChatFoldersGet/Put keep virtual folders and assignments server-side so
// they survive browser storage clears and can be used by the same-origin chat/dev UI.
func (s *Server) handleChatFoldersGet(w http.ResponseWriter, r *http.Request, id identity) {
	state := emptyChatFoldersState()
	raw, err := s.db.SettingGet(chatFoldersSettingKey(id.ID))
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			writeErr(w, http.StatusInternalServerError, "сохранённые папки повреждены")
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	normalizeChatFoldersState(&state)
	writeJSON(w, http.StatusOK, map[string]any{"folders": state})
}

func (s *Server) handleChatFoldersPut(w http.ResponseWriter, r *http.Request, id identity) {
	r.Body = http.MaxBytesReader(w, r.Body, chatFoldersStateMaxBytes)
	var state chatFoldersState
	if err := json.NewDecoder(r.Body).Decode(&state); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON папок")
		return
	}
	if err := normalizeAndValidateChatFoldersState(&state); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось сериализовать папки")
		return
	}
	if len(encoded) > chatFoldersStateMaxBytes {
		writeErr(w, http.StatusBadRequest, "настройки папок слишком большие")
		return
	}
	if err := s.db.SettingSet(chatFoldersSettingKey(id.ID), string(encoded)); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Audit(id.Login, "chat.folders.update", strconv.FormatInt(id.ID, 10), fmt.Sprintf("folders=%d assignments=%d", len(state.Folders), len(state.Assign)))
	writeJSON(w, http.StatusOK, map[string]any{"folders": state})
}

func normalizeChatFoldersState(state *chatFoldersState) {
	if state.Folders == nil {
		state.Folders = []string{}
	}
	if state.Assign == nil {
		state.Assign = map[string]string{}
	}
	if state.Settings == nil {
		state.Settings = map[string]chatFolderSetting{}
	}
}

func normalizeAndValidateChatFoldersState(state *chatFoldersState) error {
	normalizeChatFoldersState(state)
	if len(state.Folders) > maxChatFoldersPerUser || len(state.Assign) > maxChatFolderAssignments {
		return fmt.Errorf("слишком много папок или сессий")
	}
	seen := map[string]bool{}
	for i, name := range state.Folders {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 80 || strings.ContainsAny(name, "\r\n") {
			return fmt.Errorf("некорректное имя папки")
		}
		if seen[name] {
			return fmt.Errorf("папка «%s» указана несколько раз", name)
		}
		seen[name] = true
		state.Folders[i] = name
	}
	for sessionID, folder := range state.Assign {
		if sessionID == "" || len(sessionID) > 160 {
			delete(state.Assign, sessionID)
			continue
		}
		folder = strings.TrimSpace(folder)
		if !seen[folder] {
			delete(state.Assign, sessionID)
			continue
		}
		state.Assign[sessionID] = folder
	}
	for name, setting := range state.Settings {
		if !seen[name] {
			delete(state.Settings, name)
			continue
		}
		if len(setting.Instructions) > maxFolderInstructionBytes {
			return fmt.Errorf("инструкции папки «%s» слишком длинные", name)
		}
		if strings.Contains(setting.Instructions, folderInstructionStart) || strings.Contains(setting.Instructions, folderInstructionEnd) {
			return fmt.Errorf("инструкции папки «%s» содержат служебный маркер", name)
		}
	}
	if !seen[state.Active] {
		state.Active = ""
	}
	if p := state.PendingFolder; p != nil && !seen[strings.TrimSpace(p.Name)] {
		state.PendingFolder = nil
	}
	return nil
}

const (
	folderInstructionStart    = "<!-- agctl-folder-instructions:start -->"
	folderInstructionEnd      = "<!-- agctl-folder-instructions:end -->"
	maxFolderInstructionBytes = 16 << 10
)

// handleChatFolderInstructions stores virtual-project instructions in each
// session's AGENTS.local.md. DSH's built-in agent-instructions plugin loads the
// file as workspace context on first request/resume; folder grouping itself
// remains browser-local and does not create a DSH workspace directory.
func (s *Server) handleChatFolderInstructions(w http.ResponseWriter, r *http.Request, id identity) {
	sessionID := r.PathValue("sessionID")
	if !validChatSessionID(sessionID) {
		writeErr(w, http.StatusBadRequest, "неверный id чат-сессии")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFolderInstructionBytes+4096)
	var req struct {
		Folder       string `json:"folder"`
		Instructions string `json:"instructions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON")
		return
	}
	req.Folder = strings.TrimSpace(req.Folder)
	req.Instructions = strings.TrimSpace(req.Instructions)
	if len(req.Folder) > 80 || strings.ContainsAny(req.Folder, "\r\n") {
		writeErr(w, http.StatusBadRequest, "имя папки слишком длинное или содержит перенос строки")
		return
	}
	if len(req.Instructions) > maxFolderInstructionBytes || strings.Contains(req.Instructions, folderInstructionStart) || strings.Contains(req.Instructions, folderInstructionEnd) {
		writeErr(w, http.StatusBadRequest, "инструкции слишком длинные или содержат служебный маркер")
		return
	}

	sessionDir, err := safeChatSessionDir(s.cfg.Home, id.ID, sessionID, req.Instructions != "")
	if err != nil {
		code := http.StatusNotFound
		if strings.Contains(err.Error(), "symlink") || errors.Is(err, errInvalidChatSessionPath) {
			code = http.StatusBadRequest
		}
		writeErr(w, code, "не удалось безопасно разрешить каталог чат-сессии")
		return
	}
	path := filepath.Join(sessionDir, "AGENTS.local.md")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		writeErr(w, http.StatusBadRequest, "AGENTS.local.md не должен быть символической ссылкой")
		return
	}
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать инструкции: "+err.Error())
		return
	}
	updated, err := mergeFolderInstructions(old, req.Folder, req.Instructions)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if string(updated) == string(old) {
		writeJSON(w, http.StatusOK, map[string]any{"sessionId": sessionID, "folder": req.Folder, "updated": false})
		return
	}
	if strings.TrimSpace(string(updated)) == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			writeErr(w, http.StatusInternalServerError, "не удалось удалить пустой файл инструкций")
			return
		}
	} else if err := atomicWriteFile(sessionDir, path, updated, 0o644); err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось сохранить инструкции: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId": sessionID, "folder": req.Folder, "updated": true,
		"instructionsFile": "AGENTS.local.md",
		"refreshHint":      "инструкции подхватятся при следующем чтении файлов или повторном открытии сессии",
	})
}

var errInvalidChatSessionPath = errors.New("invalid chat session path")

func safeChatSessionDir(home string, userID int64, sessionID string, create bool) (string, error) {
	if !validChatSessionID(sessionID) {
		return "", errInvalidChatSessionPath
	}
	canonicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", err
	}
	canonicalHome, err = filepath.Abs(canonicalHome)
	if err != nil {
		return "", err
	}
	chatsRoot := filepath.Join(canonicalHome, "chats")
	userRoot := filepath.Join(chatsRoot, chatStorageID(userID))
	workRoot := filepath.Join(userRoot, "work")
	for _, dir := range []string{chatsRoot, userRoot, workRoot} {
		info, err := os.Lstat(dir)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("symlink or non-directory in chat path: %s", dir)
		}
	}
	sessionsRoot := filepath.Join(workRoot, ".sessions")
	info, err := os.Lstat(sessionsRoot)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(sessionsRoot, 0o700); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err = os.Lstat(sessionsRoot)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("symlink or non-directory in chat path: %s", sessionsRoot)
	}
	sessionDir := filepath.Join(sessionsRoot, sessionID)
	if !pathUnder(sessionsRoot, sessionDir) {
		return "", errInvalidChatSessionPath
	}
	info, err = os.Lstat(sessionDir)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(sessionDir, 0o700); err != nil && !os.IsExist(err) {
			return "", err
		}
		info, err = os.Lstat(sessionDir)
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", fmt.Errorf("symlink or non-directory in chat path: %s", sessionDir)
	}
	return sessionDir, nil
}

func validChatSessionID(id string) bool {
	if !strings.HasPrefix(id, "session-") || len(id) > 120 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return filepath.Base(id) == id
}

func mergeFolderInstructions(existing []byte, folder, instructions string) ([]byte, error) {
	if instructions != "" && folder == "" {
		return nil, fmt.Errorf("сначала выберите папку")
	}
	text := string(existing)
	start := strings.Index(text, folderInstructionStart)
	end := strings.Index(text, folderInstructionEnd)
	if (start < 0) != (end < 0) || start >= 0 && end < start {
		return nil, fmt.Errorf("AGENTS.local.md содержит неполный блок agctl — сохранение отменено")
	}
	if start >= 0 {
		end += len(folderInstructionEnd)
		text = text[:start] + strings.TrimLeft(text[end:], "\r\n")
	}
	if instructions == "" {
		return []byte(strings.TrimRight(text, "\r\n")), nil
	}
	block := folderInstructionStart + "\n## Папка «" + folder + "»\n\n" + instructions + "\n" + folderInstructionEnd
	if strings.TrimSpace(text) == "" {
		return []byte(block + "\n"), nil
	}
	return []byte(strings.TrimRight(text, "\r\n") + "\n\n" + block + "\n"), nil
}

func atomicWriteFile(dir, target string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(dir, ".agctl-folder-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

// userProvider — LLM-провайдер для личного чата: личный секрет пользователя,
// иначе платформенный провайдер по умолчанию (корпоративный LiteLLM); только
// если не задан ни один — dsh покажет свой модал «Add an API key».
func (s *Server) userProvider(userID int64) provision.Provider {
	sec, err := s.db.SecretByUser(userID)
	if err == nil {
		key, decErr := s.sec.Decrypt(sec.Ciphertext)
		if decErr == nil && key != "" {
			models := sec.Models
			if len(models) == 0 {
				models = []string{sec.Model}
			}
			return provision.Provider{BaseURL: sec.BaseURL, Model: sec.Model, Models: models, APIKey: key}
		}
	}
	return s.cfg.DefaultLLM
}

// ensureChat asks the runtime provider to ensure the user's stable Chat runtime
// is running. First-start locking is per user; different users start in parallel.
func (s *Server) ensureChat(userID int64) (*chatRuntime, error) {
	s.chatMu.Lock()
	if s.chats == nil {
		s.chats = make(map[int64]*chatRuntime)
	}
	if s.chatLocks == nil {
		s.chatLocks = make(map[int64]*sync.Mutex)
	}
	if rt, ok := s.chats[userID]; ok {
		rt.lastSeen = time.Now()
		s.chatMu.Unlock()
		return rt, nil
	}
	lock := s.chatLocks[userID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.chatLocks[userID] = lock
	}
	s.chatMu.Unlock()

	lock.Lock()
	defer lock.Unlock()
	s.chatMu.Lock()
	if rt, ok := s.chats[userID]; ok {
		rt.lastSeen = time.Now()
		s.chatMu.Unlock()
		return rt, nil
	}
	s.chatMu.Unlock()
	if s.chatProvider == nil {
		return nil, fmt.Errorf("chat runtime provider is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	spec := agctlrt.ChatSpec{
		RuntimeID: s.chatRuntimeID(userID), StorageID: chatStorageID(userID),
		UserID: userID, Provider: s.userProvider(userID), SessionScope: true,
	}
	handle, err := s.chatProvider.EnsureChat(ctx, spec)
	if err != nil {
		return nil, err
	}
	if handle.RuntimeID == "" || handle.Endpoint == "" {
		return nil, fmt.Errorf("chat runtime provider returned an incomplete handle")
	}
	rt := &chatRuntime{handle: handle, lastSeen: time.Now()}
	s.chatMu.Lock()
	s.chats[userID] = rt
	s.chatMu.Unlock()
	log.Printf("[chat] пользователь %d: личный runtime поднят (%s)", userID, handle.Endpoint)
	return rt, nil
}

// reapIdleChats stops per-user compute after the idle TTL. The ChatProvider
// contract requires persistent StorageID data to survive StopChat.
func (s *Server) reapIdleChats() {
	now := time.Now()
	type expiredChat struct {
		userID int64
		handle agctlrt.ChatHandle
	}
	var expired []expiredChat
	s.chatMu.Lock()
	for uid, rt := range s.chats {
		if rt.active == 0 && shouldSleepChat(rt.lastSeen, now, s.cfg.ChatTTL) {
			expired = append(expired, expiredChat{userID: uid, handle: rt.handle})
			delete(s.chats, uid)
		}
	}
	s.chatMu.Unlock()
	for _, item := range expired {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		s.forgetToken(chatTokenCacheKey(item.handle))
		err := s.chatProvider.StopChat(ctx, item.handle)
		cancel()
		if err != nil {
			log.Printf("[chat] reaper: stop user %d: %v", item.userID, err)
		} else {
			log.Printf("[chat] reaper: пользователь %d уснул (runtime удалён, volume сохранён)", item.userID)
		}
	}
}

// isDialFailure — прокси вообще не достучался до рантайма (контейнер ещё
// грузится или уже мёртв). Обрыв уже открытого соединения (стрим/WS) сюда не
// попадает: там Op "read"/"write", и пересоздавать рантайм из-за него нельзя.
func isDialFailure(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// markChatDown фиксирует первый dial-отказ рантайма и сообщает, истёк ли grace.
// Возвращает false, если запись уже не актуальна (выселена/заменена) или отказ
// случился внутри grace — второй случай отличает нормальный прогрев dsh от
// мёртвого кэша и не должен приводить к пересозданию.
func (s *Server) markChatDown(userID int64, rt *chatRuntime) bool {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chats[userID] != rt {
		return false
	}
	if rt.downSince.IsZero() {
		rt.downSince = time.Now()
		return false
	}
	return time.Since(rt.downSince) >= chatDownGrace
}

// evictChat забывает мёртвый runtime. Контейнер не трогаем (его может уже не
// быть), персистентные данные живут на маунтах: следующий /chat/ пройдёт через
// ensureChat → EnsureChat, которая идемпотентно пересоздаёт контейнер.
func (s *Server) evictChat(userID int64, rt *chatRuntime) {
	s.chatMu.Lock()
	if s.chats[userID] == rt {
		delete(s.chats, userID)
	}
	s.chatMu.Unlock()
	log.Printf("[chat] пользователь %d: runtime %s недоступен дольше %s — кэш сброшен, контейнер пересоздастся при следующем открытии чата", userID, rt.handle.Endpoint, chatDownGrace)
}

// writeWarmChatPage — страница авто-повтора, пока личный чат прогревается.
func writeWarmChatPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>agctl: прогрев</title><meta http-equiv="refresh" content="2"><p style="font-family:system-ui;padding:2rem">Личный чат запускается… <a href="/chat/">Повторить сейчас</a></p>`)
}

// rootEntry — корень платформы: залогинен → свой чат, иначе → SPA-логин.
func (s *Server) rootEntry(w http.ResponseWriter, r *http.Request) {
	if _, err := s.who(r); err != nil {
		http.Redirect(w, r, "/app/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
}

// appEntry — SPA кабинета (логин и администрирование) на отдельном пути:
// корень платформы занят чатом.
func (s *Server) appEntry(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/", http.StatusPermanentRedirect)
}

// chatProxy — gateway for the authenticated user's runtime. Endpoint is
// provider-owned (loopback for Docker; internal Service DNS for Kubernetes).
func (s *Server) chatProxy(w http.ResponseWriter, r *http.Request, id identity) {
	rt, err := s.ensureChat(id.ID)
	if err != nil {
		log.Printf("[chat] ensure user %d: %v", id.ID, err)
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			writeWarmChatPage(w)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "чат ещё прогревается, обнови страницу через пару секунд")
		return
	}
	s.chatMu.Lock()
	if current := s.chats[id.ID]; current != rt {
		s.chatMu.Unlock()
		writeErr(w, http.StatusServiceUnavailable, "чат усыплён, обнови страницу")
		return
	}
	rt.active++
	rt.lastSeen = time.Now()
	s.chatMu.Unlock()
	defer func() {
		s.chatMu.Lock()
		if s.chats[id.ID] == rt {
			rt.active--
			rt.lastSeen = time.Now()
		}
		s.chatMu.Unlock()
	}()
	if origin := r.Header.Get("Origin"); origin != "" && !sameOriginAuthority(origin, r.Host) {
		writeErr(w, http.StatusForbidden, "cross-origin запрос к чату запрещён")
		return
	}

	target, err := url.Parse(rt.handle.Endpoint)
	if err != nil || target.Scheme == "" || target.Host == "" {
		writeErr(w, http.StatusBadGateway, "chat runtime endpoint is invalid")
		return
	}
	authority := target.Host
	const prefix = "/chat"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			stripAgctlCookies(pr.Out)
			// Не просим сжатие: чат-шелл встраивается в HTML до отдачи браузеру,
			// а gzip/br тело нельзя безопасно менять без потери прокси-потока.
			// DSH runtime локальный; экономия нескольких KB не стоит разных
			// (curl vs браузер) версий документа.
			pr.Out.Header.Set("Accept-Encoding", "identity")
			pr.Out.Host = authority
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", pr.Out.URL.Scheme+"://"+authority)
			}
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			if !strings.HasPrefix(pr.Out.URL.Path, "/") {
				pr.Out.URL.Path = "/" + pr.Out.URL.Path
			}
			pr.Out.URL.RawQuery = stripTokenParam(pr.In.URL.RawQuery)
		},
		FlushInterval: -1,
		Transport: &mintTransport{
			base:  proxyTransport,
			token: func() string { return s.chatToken(r.Context(), rt.handle) },
		},
		ModifyResponse: func(resp *http.Response) error {
			// Рантайм ответил — он живой: гасим счётчик dial-отказов.
			s.chatMu.Lock()
			rt.downSince = time.Time{}
			s.chatMu.Unlock()
			// Pre-paint чат-шелл — bounded, identity-encoded HTML; при слишком
			// большом или ошибочно читаемом документе helper возвращает исходный поток.
			injectChatShellResponse(resp)
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", rewriteLocation(loc, prefix, authority))
			}
			// dsh-auth cookie ограничивается /chat/, чтобы не пересылаться
			// на кабинет API и dev-workspaces /w/<id>/.
			cookies := resp.Cookies()
			if len(cookies) > 0 {
				resp.Header.Del("Set-Cookie")
				for _, c := range cookies {
					if strings.HasPrefix(c.Name, "dsh-auth-") {
						if c.Path == "" || c.Path == "/" {
							c.Path = prefix + "/"
						}
						if s.cfg.SecureCookies {
							c.Secure = true
						}
					}
					resp.Header.Add("Set-Cookie", c.String())
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[chat] proxy user %d: %v", id.ID, err)
			if isDialFailure(err) && s.markChatDown(id.ID, rt) {
				s.evictChat(id.ID, rt)
			}
			if strings.Contains(r.Header.Get("Accept"), "text/html") {
				writeWarmChatPage(w)
				return
			}
			writeErr(w, http.StatusBadGateway, "чат недоступен")
		},
	}
	proxy.ServeHTTP(w, r)
}

func chatTokenCacheKey(handle agctlrt.ChatHandle) string {
	return "chat/" + handle.RuntimeID + "/" + handle.InstanceID
}
