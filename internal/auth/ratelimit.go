package auth

// ratelimit.go — ограничение частоты регистраций с одного адреса (дефект D3
// из протокола ручного прогона: 25 аккаунтов за 0,5 с). Окно скользящее:
// держим последние попадания ключа и отбрасываем вышедшие за окно.

import (
	"sync"
	"time"
)

// RegisterLimiter — «не более limit регистраций за window по ключу»
// (ключ — адрес клиента). Потокобезопасен; часы подменяемы для тестов.
type RegisterLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	hits   map[string][]time.Time
}

// NewRegisterLimiter — лимит > 0 и окно > 0; сервис создаёт limiter только
// когда защита включена (Config.RegisterPerMinute >= 0).
func NewRegisterLimiter(limit int, window time.Duration) *RegisterLimiter {
	return &RegisterLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   map[string][]time.Time{},
	}
}

// Allow — разрешить регистрацию с ключа key. false, retryAfter — лимит
// исчерпан, повторить можно через retryAfter.
func (l *RegisterLimiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	kept := l.hits[key][:0]
	for _, ts := range l.hits[key] {
		if now.Sub(ts) < l.window {
			kept = append(kept, ts)
		}
	}
	l.hits[key] = kept
	if len(kept) >= l.limit {
		return false, l.window - now.Sub(kept[0])
	}
	l.hits[key] = append(kept, now)
	return true, 0
}
