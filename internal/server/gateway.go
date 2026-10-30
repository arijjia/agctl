package server

// Обратный прокси воркспейсов: /w/{id}/ → 127.0.0.1:<hostPort>; для UI/API
// и WebSocket/SSE проверяется сессия agctl. Только статический manifest публичен.
//
// Boot-токен dsh web в браузер не отдаётся: при upstream-401 транспорт
// повторяет запрос серверно с ?token=<boot> — у dsh 0.2.0+ это нативный
// cookie-mint (303 + Set-Cookie dsh-auth-*, HttpOnly, SameSite=Strict);
// Location переписывается на префикс /w/{id}/, cookie прозрачно доставляется
// браузеру и дальше работает сама. Рестарт контейнера (новый токен, новый
// порт) само-лечится тем же повторным mint-ом.
//
// Тест: gateway_test.go (мок upstream с той же схемой 401→303+cookie).

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/store"
)

// workspaceProxyEntry — GET /w/{id} без слэша: на канонический префикс.
func (s *Server) workspaceProxyEntry(w http.ResponseWriter, r *http.Request, _ identity) {
	id := r.PathValue("id")
	target := url.URL{Path: "/w/" + id + "/"}
	q := r.URL.Query()
	q.Del("token") // не сохраняем boot-token в адресной строке
	target.RawQuery = q.Encode()
	http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
}

// workspaceProxy — /w/{id}/...: авторизованный прокси в контейнер для DSH UI и API.
func (s *Server) workspaceProxy(w http.ResponseWriter, r *http.Request, id identity) {
	ws, err := s.loadWS(r, id)
	if errors.Is(err, errForbidden) {
		writeErr(w, http.StatusForbidden, "нет доступа к этому воркспейсу")
		return
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	if !visibleTo(ws, id) {
		writeErr(w, http.StatusForbidden, "нет доступа к этому воркспейсу")
		return
	}
	s.proxyWorkspace(w, r, ws)
}

// workspaceManifest — публичный только для чтения webmanifest: браузеры
// загружают его без auth-cookie. Остальные пути workspace требуют agctl-сессию.
func (s *Server) workspaceManifest(w http.ResponseWriter, r *http.Request) {
	ws, err := s.db.WorkspaceByID(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "воркспейс не найден")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось прочитать воркспейс")
		return
	}
	s.proxyWorkspace(w, r, ws)
}

// proxyWorkspace проверяет состояние и проксирует один путь воркспейса.
func (s *Server) proxyWorkspace(w http.ResponseWriter, r *http.Request, ws store.Workspace) {
	if ws.Status != "running" || ws.HostPort == 0 {
		// Браузеру (клик по stopped-месту в «РЕЖИМАХ») — брендированная
		// страница с кнопкой запуска; API-клиенту — прежний JSON.
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, stoppedWorkspacePage(ws, s.brand))
			return
		}
		writeErr(w, http.StatusConflict, "воркспейс не запущен (статус: "+ws.Status+")")
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && !sameOriginAuthority(origin, r.Host) {
		writeErr(w, http.StatusForbidden, "cross-origin запрос к воркспейсу запрещён")
		return
	}

	wsID := ws.ID
	target := "127.0.0.1:" + strconv.Itoa(ws.HostPort)
	prefix := "/w/" + wsID
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{Scheme: "http", Host: target})
			stripAgctlCookies(pr.Out)
			// dev-шелл брендинга вставляется в HTML на лету — сжатие от
			// upstream не пропускаем: инъекция требует чистого текста.
			pr.Out.Header.Set("Accept-Encoding", "identity")
			// Host фиксированный: dsh привязывает mint-cookie к authority,
			// константный хост делает cookie валидной для всех запросов.
			pr.Out.Host = target
			// DSH проверяет Host/Origin на каждом API и WebSocket-запросе.
			// Снаружи Origin обязан совпадать с Host agctl (проверено выше);
			// внутри переписываем Origin на authority контейнера.
			if pr.In.Header.Get("Origin") != "" {
				pr.Out.Header.Set("Origin", pr.Out.URL.Scheme+"://"+target)
			}
			pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
			if !strings.HasPrefix(pr.Out.URL.Path, "/") {
				pr.Out.URL.Path = "/" + pr.Out.URL.Path
			}
			// Boot-токен принимает только серверный mint transport; не
			// передаём произвольный ?token= от браузера upstream.
			// Query пересылаем побайтово: combo-URL плагинов dsh
			// (/plugins/??модули&rev=…) парсятся upstream как сырая строка
			// с ведущим литеральным '?'; разбор с повторным кодированием
			// (Query().Encode()) превращает '?' в %3F, дописывает '=' и
			// пересортировывает ключи — dsh отвечает 404, и браузер падает
			// «HTML did not preload …/dsh-client-modules/client.js».
			pr.Out.URL.RawQuery = stripTokenParam(pr.In.URL.RawQuery)
		},
		FlushInterval: -1, // немедленно flush-ить SSE/потоки агента
		Transport: &mintTransport{
			base: proxyTransport,
			token: func() string {
				tok := s.wsToken(r.Context(), ws)
				if tok == "" {
					log.Printf("[gateway] %s: boot-токен недоступен", wsID)
				}
				return tok
			},
		},
		ModifyResponse: func(resp *http.Response) error {
			// Брендинг dev-места: тот же подход, что чат-шелл на /chat/, но
			// только заголовок/лого/тэглайн — инструменты разработчика не
			// трогаем (панели, Workspaces, выборщик моделей).
			injectDevShellResponse := s.injectDevShellResponse
			injectDevShellResponse(resp)
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", rewriteLocation(loc, prefix, target))
			}
			// Cookie DSH действует только в пределах этого workspace; она
			// не рассылается браузером к API agctl и соседним средам.
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
			log.Printf("[gateway] %s: %v", wsID, err)
			// Браузеру — страница с авто-ретраем: контейнер после старта
			// прогревается несколько секунд (dsh-warmup-indicator); API-клиенту — JSON.
			if strings.Contains(r.Header.Get("Accept"), "text/html") {
				canonical := prefix + "/"
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>agctl: прогрев</title><meta http-equiv="refresh" content="3"><p style="font-family:system-ui;padding:2rem">Рабочее место запускается… страница обновится автоматически. <a href="%s">Повторить сейчас</a></p>`, canonical)
				return
			}
			writeErr(w, http.StatusBadGateway, "воркспейс недоступен")
		},
	}
	http.StripPrefix(prefix, proxy).ServeHTTP(w, r)
}

// sameOriginAuthority prevents the gateway from erasing a cross-site Origin
// before DSH's own Host/Origin fence sees it.
func sameOriginAuthority(origin, authority string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	host, err := url.Parse("//" + authority)
	if err != nil || host.Host == "" {
		return false
	}
	originPort, hostPort := u.Port(), host.Port()
	if originPort == "" {
		originPort = defaultPort(u.Scheme)
	}
	if hostPort == "" {
		hostPort = defaultPort(u.Scheme)
	}
	return strings.EqualFold(u.Hostname(), host.Hostname()) && originPort == hostPort
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// mintTransport — повторяет запрос с ?token=<boot> при upstream-401
// (нативный cookie-mint dsh); token добавляется только на backend→upstream.
type mintTransport struct {
	base  http.RoundTripper
	token func() string
}

func (t *mintTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	tok := t.token()
	if tok == "" {
		return resp, nil
	}

	retry := req.Clone(req.Context())
	q := retry.URL.Query()
	q.Set("token", tok)
	retry.URL.RawQuery = q.Encode()
	minted, err := t.base.RoundTrip(retry)
	if err != nil {
		return resp, nil // оставляем исходный 401 с его телом
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return minted, nil
}

// stripTokenParam — удаляет параметр token из сырой строки query, сохраняя
// остальные байты как есть: разбор с повторным кодированием ломает combo-URL
// dsh (ведущий '?' в query, порядок и экранирование модулей).
func stripTokenParam(rawQuery string) string {
	if !strings.Contains(rawQuery, "token=") && !strings.HasSuffix(rawQuery, "&token") && rawQuery != "token" {
		return rawQuery
	}
	kept := make([]string, 0, strings.Count(rawQuery, "&")+1)
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "token" || strings.HasPrefix(pair, "token=") {
			continue
		}
		kept = append(kept, pair)
	}
	return strings.Join(kept, "&")
}

// stripAgctlCookies prevents platform authentication credentials from crossing
// the reverse-proxy boundary into a DSH runtime. DSH's own scoped cookies and
// unrelated client cookies remain available to the upstream.
func stripAgctlCookies(req *http.Request) {
	cookies := req.Cookies()
	if len(cookies) == 0 {
		return
	}
	req.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == "agctl_session" || strings.HasPrefix(cookie.Name, "agctl_") {
			continue
		}
		req.AddCookie(cookie)
	}
}

// rewriteLocation — внутренний Location upstream ведёт на корень ("/") или
// абсолютный URL этого же upstream; браузер должен остаться на /w/{id}/.
// Внешние абсолютные URL не переписываются. prefix — "/w/<id>" без слэша.
func rewriteLocation(loc, prefix, upstreamHost string) string {
	if u, err := url.Parse(loc); err == nil && u.Host != "" {
		if u.Host != upstreamHost {
			return loc
		}
		loc = u.RequestURI()
	}
	if loc == prefix || strings.HasPrefix(loc, prefix+"/") || strings.HasPrefix(loc, prefix+"?") {
		return loc
	}
	if rest := strings.TrimPrefix(loc, "/"); rest != "" {
		return prefix + "/" + rest
	}
	return prefix + "/"
}

// proxyTransport — общий транспорт прокси (лимиты дефолтные, WS/SSE проходит).
var proxyTransport = http.DefaultTransport.(*http.Transport).Clone()
