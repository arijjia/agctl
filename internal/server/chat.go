package server

// chat.go — режим 1 «Чат»: ленивый персональный dsh-рантайм на пользователя.
//
// Персистентность = том dsh-home пользователя (сессии, файлы, credentials);
// контейнер — убиваемая песочница: лениво поднимается при обращении к /chat/,
// останавливается и удаляется по простою (ChatTTL). После удаления том
// остаётся — новый контейнер продолжает с теми же сессиями.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
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
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/store"
)

const chatNamePrefix = "chat"

// chatRuntime — живой персональный чат-контейнер пользователя.
type chatRuntime struct {
	port        int
	containerID string
	lastSeen    time.Time
	active      int // streaming HTTP/WS requests; reaper must not stop active sessions
}

// shouldSleepChat — решение рипера: контейнер простаивал дольше TTL.
func shouldSleepChat(lastSeen, now time.Time, ttl time.Duration) bool {
	return now.Sub(lastSeen) >= ttl
}

// userChatDirs — персистентные пути пользователя (том).
func (s *Server) userChatDirs(userID int64) (dshHome, work string) {
	base := filepath.Join(s.cfg.Home, "chats", "u"+strconv.FormatInt(userID, 10))
	return filepath.Join(base, "dsh-home"), filepath.Join(base, "work")
}

// chatRuntimeID namespaces Docker container names by agctl home, so multiple
// isolated agctl deployments can safely share one Docker daemon.
func (s *Server) chatRuntimeID(userID int64) string {
	sum := sha256.Sum256([]byte(filepath.Clean(s.cfg.Home)))
	return fmt.Sprintf("%s-%x-u%d", chatNamePrefix, sum[:4], userID)
}

// userProvider — LLM-провайдер из личного секрета пользователя. Нет секрета —
// dsh покажет свой модал «Add an API key» (ключ сохранится в dsh-home юзера).
func (s *Server) userProvider(userID int64) provision.Provider {
	sec, err := s.db.SecretByUser(userID)
	if err != nil {
		return provision.Provider{}
	}
	key, err := s.sec.Decrypt(sec.Ciphertext)
	if err != nil || key == "" {
		return provision.Provider{}
	}
	models := sec.Models
	if len(models) == 0 {
		models = []string{sec.Model}
	}
	return provision.Provider{BaseURL: sec.BaseURL, Model: sec.Model, Models: models, APIKey: key}
}

// ensureChat поднимает (или возвращает живой) персональный чат-контейнер.
// Первый старт сериализован per-user, разные пользователи стартуют параллельно.
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

	dshHome, work := s.userChatDirs(userID)
	for _, dir := range []string{dshHome, work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	provider := s.userProvider(userID)
	if _, err := os.Stat(filepath.Join(dshHome, "profiles", "web", "package.json")); err != nil {
		if err := provision.SeedDshHome(dshHome, provider, assetsDshSkel); err != nil {
			return nil, err
		}
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		return nil, err
	}
	layout := provision.Layout{Work: work, DshHome: dshHome, ChatSessionScoped: true}
	name := s.chatRuntimeID(userID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	res, err := s.rt.Start(ctx, layout, name, dockerrt.StartOpts{HostPort: port}, provider)
	if err != nil {
		return nil, err
	}
	rt := &chatRuntime{port: port, containerID: res.ContainerID, lastSeen: time.Now()}
	s.chatMu.Lock()
	s.chats[userID] = rt
	s.chatMu.Unlock()
	log.Printf("[chat] пользователь %d: личный чат поднят на 127.0.0.1:%d", userID, port)
	return rt, nil
}

// reapIdleChats останавливает и удаляет контейнеры чатов, простаивавшие дольше
// TTL. Тома dsh-home остаются — следующий запрос поднимает контейнер заново
// с теми же сессиями (warm start ~2 с).
func (s *Server) reapIdleChats() {
	now := time.Now()
	var expired []int64
	var containers []string
	s.chatMu.Lock()
	for uid, rt := range s.chats {
		if rt.active == 0 && shouldSleepChat(rt.lastSeen, now, s.cfg.ChatTTL) {
			expired = append(expired, uid)
			containers = append(containers, rt.containerID)
			delete(s.chats, uid)
		}
	}
	s.chatMu.Unlock()
	for i, uid := range expired {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := s.rt.Stop(ctx, containers[i]); err != nil {
			log.Printf("[chat] reaper: stop user %d: %v", uid, err)
		} else {
			log.Printf("[chat] reaper: пользователь %d уснул (контейнер удалён, том сохранён)", uid)
		}
		cancel()
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

// chatProxy — шлюз персонального чата: ensure рантайма, затем тот же
// mint/cookie-паттерн, что и /w/{id}/.
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

	target := "127.0.0.1:" + strconv.Itoa(rt.port)
	const prefix = "/chat"
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: target})
			pr.Out.Host = target
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", pr.Out.URL.Scheme+"://"+target)
			}
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			if !strings.HasPrefix(pr.Out.URL.Path, "/") {
				pr.Out.URL.Path = "/" + pr.Out.URL.Path
			}
			pr.Out.URL.RawQuery = stripTokenParam(pr.In.URL.RawQuery)
		},
		FlushInterval: -1,
		Transport: &mintTransport{
			base: proxyTransport,
			token: func() string {
				return s.wsToken(r.Context(), store.Workspace{ID: s.chatRuntimeID(id.ID), ContainerID: rt.containerID})
			},
		},
		ModifyResponse: func(resp *http.Response) error {
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", rewriteLocation(loc, prefix, target))
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
