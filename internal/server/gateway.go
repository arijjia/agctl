package server

// Транспорт и хелперы обратного прокси чат-контейнеров (chat.go):
// cookie-mint при upstream-401 (?token=<boot>), срезка agctl-cookie и
// token-параметра, переписывание Location на префикс прокси.
//
// Тест: gateway_test.go (мок upstream с той же схемой 401→303+cookie).

import (
	"io"
	"net/http"
	"net/url"
	"strings"
)

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

// defaultPort — порт по схеме upstream: 443 для https, иначе 80.
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

// RoundTrip повторяет запрос с ?token=<boot> при upstream-401 (нативный
// cookie-mint dsh); токен добавляется только на участке backend→upstream.
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
