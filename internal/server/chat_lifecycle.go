package server

import (
	"context"
	"errors"
	"html"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Жизненный цикл рабочего места вокруг ensureChat: остановка владельцем,
// ограниченное ожидание старта в прокси и пауза после сбоя старта.
//
// Остановка владельцем (кнопка «Остановить») запоминается: открытая вкладка
// dsh переподключается через прокси, и без флага ensureChat тут же создавал
// контейнер заново — «остановить» не работало. Флаг живёт в памяти (перезапуск
// agctl его снимает) и снимается явным стартом (startChat).
//
// Прокси больше не ждёт старт без предела: дольше chatProxyStartWait запрос
// получает страницу ожидания. Иначе каждая вкладка держала соединение до конца
// старта (а он может включать скачивание или сборку образа), браузер упирался
// в свои шесть соединений на хост, и вставали все страницы лаунчера.

var (
	// errChatStopped — место остановлено владельцем; само не поднимается.
	errChatStopped = errors.New("рабочее место остановлено — запустите его в лаунчере")
	// errChatStarting — старт идёт дольше, чем прокси готов ждать.
	errChatStarting = errors.New("рабочее место запускается")
)

const (
	// chatProxyStartWait — сколько запрос прокси ждёт старт места.
	chatProxyStartWait = 20 * time.Second
	// chatRetryAfter — пауза после сбоя старта, прежде чем прокси попробует
	// снова: сломанный старт не должен повторяться на каждое обновление
	// страницы ожидания (раз в 2 секунды).
	chatRetryAfter = 30 * time.Second
)

// chatFlight — старт слота, идущий в фоне; done закрывается по завершении.
type chatFlight struct {
	done chan struct{}
	rt   *chatRuntime
	err  error
}

// chatFail — последний сбой старта слота (для паузы и страницы ошибки).
type chatFail struct {
	msg string
	at  time.Time
}

// chatKeyLock — мьютекс слота: старт (ensureChat) и остановка (stopChatKey)
// одного места не пересекаются.
func (s *Server) chatKeyLock(key chatKey) *sync.Mutex {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chatLocks == nil {
		s.chatLocks = make(map[chatKey]*sync.Mutex)
	}
	lock := s.chatLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.chatLocks[key] = lock
	}
	return lock
}

func (s *Server) chatIsStopped(key chatKey) bool {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	return s.chatStopped[key]
}

func (s *Server) setChatStopped(key chatKey, stopped bool) {
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	if s.chatStopped == nil {
		s.chatStopped = make(map[chatKey]bool)
	}
	if stopped {
		s.chatStopped[key] = true
	} else {
		delete(s.chatStopped, key)
	}
}

// startChat — явный старт места (кнопка лаунчера, вход, админ): снимает флаг
// остановки владельцем и паузу после сбоя, затем ensureChat.
func (s *Server) startChat(userID int64, slot int) (*chatRuntime, error) {
	key := chatKey{userID, slot}
	s.setChatStopped(key, false)
	s.chatMu.Lock()
	delete(s.chatFails, key)
	s.chatMu.Unlock()
	return s.ensureChat(userID, slot)
}

// stopChatKey — остановить рантайм слота. Кэш забывается до остановки, чтобы
// новые запросы не уходили в гаснущий контейнер; всё — под мьютексом слота,
// так что старт того же места дождётся конца остановки, а не поднимется поверх
// неё. byUser — остановка владельцем: место не поднимается само до явного
// старта. Возвращает, был ли рантайм.
func (s *Server) stopChatKey(key chatKey, byUser bool, why string) (bool, error) {
	if byUser {
		s.setChatStopped(key, true)
	}
	lock := s.chatKeyLock(key)
	lock.Lock()
	defer lock.Unlock()
	return s.stopChatKeyLocked(key, why)
}

// stopChatKeyLocked — stopChatKey для вызывающего, который уже держит мьютекс
// слота (клон места копирует dsh-home под ним).
func (s *Server) stopChatKeyLocked(key chatKey, why string) (bool, error) {
	s.chatMu.Lock()
	rt := s.chats[key]
	if rt != nil {
		delete(s.chats, key)
	}
	s.chatMu.Unlock()
	if rt == nil {
		return false, nil
	}
	s.forgetToken(chatTokenCacheKey(rt.handle))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	started := time.Now()
	err := s.chatProvider.StopChat(ctx, rt.handle)
	if err != nil {
		log.Printf("[chat] пользователь %d слот %d: остановка (%s): %v", key.uid, key.slot, why, err)
	} else {
		log.Printf("[chat] пользователь %d слот %d: остановлено (%s) за %s", key.uid, key.slot, why, time.Since(started).Round(time.Millisecond))
	}
	return true, err
}

// ensureChatForProxy — рантайм слота для запроса прокси: живой — сразу; иначе
// старт в фоне (один на слот), и запрос ждёт его не дольше wait. Остановленное
// владельцем место не поднимается (errChatStopped); после сбоя старта прокси
// chatRetryAfter возвращает тот же сбой, не повторяя старт.
func (s *Server) ensureChatForProxy(ctx context.Context, userID int64, slot int, wait time.Duration) (*chatRuntime, error) {
	if rt := s.cachedChatForEnsure(userID, slot); rt != nil {
		return rt, nil
	}
	key := chatKey{userID, slot}
	s.chatMu.Lock()
	if s.chatStopped[key] {
		s.chatMu.Unlock()
		return nil, errChatStopped
	}
	if f, ok := s.chatFails[key]; ok && time.Since(f.at) < chatRetryAfter {
		s.chatMu.Unlock()
		return nil, errors.New(f.msg)
	}
	if s.chatFlights == nil {
		s.chatFlights = make(map[chatKey]*chatFlight)
	}
	fl := s.chatFlights[key]
	if fl == nil {
		fl = &chatFlight{done: make(chan struct{})}
		s.chatFlights[key] = fl
		go s.runChatFlight(key, fl)
	}
	s.chatMu.Unlock()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-fl.done:
		return fl.rt, fl.err
	case <-timer.C:
		return nil, errChatStarting
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// runChatFlight — фоновой старт для прокси; сбой запоминается для паузы.
func (s *Server) runChatFlight(key chatKey, fl *chatFlight) {
	fl.rt, fl.err = s.ensureChat(key.uid, key.slot)
	s.chatMu.Lock()
	delete(s.chatFlights, key)
	if s.chatFails == nil {
		s.chatFails = make(map[chatKey]chatFail)
	}
	if fl.err != nil && !errors.Is(fl.err, errChatStopped) {
		s.chatFails[key] = chatFail{msg: fl.err.Error(), at: time.Now()}
	} else {
		delete(s.chatFails, key)
	}
	s.chatMu.Unlock()
	close(fl.done)
	if fl.err != nil && !errors.Is(fl.err, errChatStopped) {
		log.Printf("[chat] пользователь %d слот %d: старт не удался: %v", key.uid, key.slot, fl.err)
	}
}

// writeChatUnavailable — ответ прокси, когда рантайма нет: страница для
// браузера (ожидание, остановлено, сбой) или JSON для запросов приложения.
func writeChatUnavailable(w http.ResponseWriter, r *http.Request, prefix string, err error) {
	htmlReq := strings.Contains(r.Header.Get("Accept"), "text/html")
	switch {
	case errors.Is(err, errChatStopped):
		if htmlReq {
			writeChatPage(w, http.StatusConflict, "agctl: место остановлено", "",
				`Рабочее место остановлено и само не поднимется. Запустить его можно в <a href="/launcher">лаунчере</a>.`)
			return
		}
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, errChatStarting):
		if htmlReq {
			writeWarmChatPageAt(w, prefix)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "чат ещё прогревается, обнови страницу через пару секунд")
	default:
		if htmlReq {
			writeChatPage(w, http.StatusServiceUnavailable, "agctl: место не запустилось", "30",
				`Рабочее место не запустилось:<pre style="white-space:pre-wrap">`+html.EscapeString(err.Error())+`</pre>`+
					`Повтор — через полминуты (<a href="`+html.EscapeString(prefix)+`/">повторить сейчас</a>). Журнал старта — в <a href="/launcher">лаунчере</a> и в <code>agctl dev logs</code>.`)
			return
		}
		writeErr(w, http.StatusServiceUnavailable, "старт рабочего места: "+err.Error())
	}
}

// writeChatPage — простая страница-заглушка вместо чата; refresh — секунды
// до автоповтора ("" — без него).
func writeChatPage(w http.ResponseWriter, code int, title, refresh, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	meta := ""
	if refresh != "" {
		meta = `<meta http-equiv="refresh" content="` + refresh + `">`
	}
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>`+html.EscapeString(title)+`</title>`+meta+
		`<div style="font-family:system-ui;padding:2rem;max-width:48rem">`+body+`</div>`)
}
