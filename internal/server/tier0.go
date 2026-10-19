package server

// tier0.go — общий чат-инстанс платформы (Tier 0): один контейнер dsh web,
// в который после логина попадает любой пользователь (корень платформы —
// сразу чат, по решению владельца 2026-10-19). Создание инженерных мест —
// из панели «Кабинет» внутри этого чата.

import (
	"context"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/runtime/dockerrt"
	"github.com/arijjia/agctl/internal/store"
)

const tier0ID = "tier0"

// EnsureTier0 поднимает общий чат (если включён и ещё не запущен). Без него
// маршруты /chat отвечают 503 «прогрев». Вызывается асинхронно при старте serve.
func (s *Server) EnsureTier0(ctx context.Context) {
	if !s.cfg.Tier0 {
		return
	}
	s.tier0Mu.Lock()
	defer s.tier0Mu.Unlock()
	if s.tier0.port != 0 {
		return
	}
	home := filepath.Join(s.cfg.Home, "system", "tier0", "dsh-home")
	work := filepath.Join(s.cfg.Home, "system", "tier0", "work")
	for _, dir := range []string{home, work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("[tier0] mkdir: %v", err)
			return
		}
	}
	if err := provision.SeedDshHome(home, s.tier0Provider(), assetsDshSkel); err != nil {
		log.Printf("[tier0] seed dsh-home: %v", err)
		return
	}
	port, err := dockerrt.FreePort()
	if err != nil {
		log.Printf("[tier0] free port: %v", err)
		return
	}
	layout := provision.Layout{Work: work, DshHome: home}
	res, err := s.rt.Start(ctx, layout, tier0ID, dockerrt.StartOpts{HostPort: port}, provision.Provider{})
	if err != nil {
		log.Printf("[tier0] start: %v", err)
		return
	}
	s.tier0.port = port
	s.tier0.containerID = res.ContainerID
	log.Printf("[tier0] общий чат поднят на http://127.0.0.1:%d (контейнер %s)", port, res.ContainerID)
}

// tier0Provider — настройки LLM для общего чата: берём первый сохранённый
// секрет платформы (обычно админский). Нет секрета — dsh покажет свой
// «Add an API key» (ключ сохранится в dsh-home tier0).
func (s *Server) tier0Provider() provision.Provider {
	users, err := s.db.UsersList()
	if err == nil {
		for _, u := range users {
			if sec, err := s.db.SecretByUser(u.ID); err == nil {
				if key, err := s.sec.Decrypt(sec.Ciphertext); err == nil && key != "" {
					models := sec.Models
					if len(models) == 0 {
						models = []string{sec.Model}
					}
					return provision.Provider{BaseURL: sec.BaseURL, Model: sec.Model, Models: models, APIKey: key}
				}
			}
		}
	}
	return provision.Provider{}
}

// rootEntry — корень платформы: залогинен → общий чат, иначе → SPA-логин.
func (s *Server) rootEntry(w http.ResponseWriter, r *http.Request) {
	if _, err := s.who(r); err != nil {
		http.Redirect(w, r, "/app/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/chat/", http.StatusTemporaryRedirect)
}

// appEntry — SPA кабинета (логин и администрирование) на отдельном пути:
// корень платформы занят общим чатом.
func (s *Server) appEntry(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/app/", http.StatusPermanentRedirect)
}

// tier0Proxy — шлюз общего чата: тот же mint/cookie-паттерн, что и /w/{id}/,
// но проверяется только сессия agctl (владельцы — все залогиненные).
func (s *Server) tier0Proxy(w http.ResponseWriter, r *http.Request, id identity) {
	s.tier0Mu.Lock()
	port := s.tier0.port
	container := s.tier0.containerID
	s.tier0Mu.Unlock()
	if port == 0 {
		writeErr(w, http.StatusServiceUnavailable, "общий чат ещё прогревается, обнови страницу через пару секунд")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameOriginAuthority(origin, r.Host) {
		writeErr(w, http.StatusForbidden, "cross-origin запрос к чату запрещён")
		return
	}

	target := "127.0.0.1:" + strconv.Itoa(port)
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
				return s.wsToken(r.Context(), store.Workspace{ID: tier0ID, ContainerID: container})
			},
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[tier0] proxy: %v", err)
			writeErr(w, http.StatusBadGateway, "чат недоступен")
		},
	}
	proxy.ServeHTTP(w, r)
}
