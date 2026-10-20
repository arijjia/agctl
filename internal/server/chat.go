package server

// chat.go — режим 1 «Чат»: lazy per-user runtime behind the agctl gateway.
// The server depends on runtime.ChatProvider, not Docker ports or host mounts;
// Docker is one adapter and a Kubernetes adapter can return Service DNS + PVC.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

const chatNamePrefix = "chat"

// chatRuntime — живой персональный чат runtime пользователя.
type chatRuntime struct {
	handle   agctlrt.ChatHandle
	lastSeen time.Time
	active   int // streaming HTTP/WS requests; reaper must not stop active sessions
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
			if strings.Contains(r.Header.Get("Accept"), "text/html") {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>agctl: прогрев</title><meta http-equiv="refresh" content="2"><p style="font-family:system-ui;padding:2rem">Личный чат запускается… <a href="/chat/">Повторить сейчас</a></p>`)
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
