package server

// local_owner.go — владелец локального режима без серверной модели данных
// (карточка local-mode-owner-data-model). Владелец — не учётная запись:
// строки в users нет, пароля нет, роль developer задана конфигурацией.
// Вход — одноразовый токен из CLI (/local/login), сессии браузера — файл 0600
// в доме сервера (хеши токенов и сроки), а не таблица sessions.
//
// SQLite локально остаётся хранилищем общего кода ядра: settings (образ,
// провайдер модели, MCP и компоненты проектов) и аудит; у этих таблиц нет
// внешних ключей на users.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/arijjia/agctl/internal/auth"
)

// localOwnerID — ID владельца на новой установке. Установка, поднятая до
// этой модели, сохраняет ID своей строки в users: от него зависят пути
// chats/u<ID> (dsh-home с сессиями dsh) и ключи settings.
const localOwnerID int64 = 1

// localOwnerRole — роль владельца: свой контейнер, лаунчер, MCP.
const localOwnerRole = "developer"

// ownerSessionsFile — файл сессий владельца в доме сервера.
const ownerSessionsFile = "local-sessions.json"

// ownerSessions — сессии браузера владельца: sha256(токен) → срок. Токены
// на диске не лежат; файл переживает рестарт (dev update, dev up), поэтому
// открытая вкладка остаётся залогиненной.
type ownerSessions struct {
	mu   sync.Mutex
	path string
	m    map[string]time.Time
}

// loadOwnerSessions читает файл сессий; нет файла или он битый — пусто
// (владелец войдёт заново через agctl dev open).
func loadOwnerSessions(path string) *ownerSessions {
	o := &ownerSessions{path: path, m: map[string]time.Time{}}
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &o.m)
	}
	return o
}

func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// pruneLocked убирает истёкшие сессии; вызывается под mu.
func (o *ownerSessions) pruneLocked(now time.Time) {
	for k, exp := range o.m {
		if !now.Before(exp) {
			delete(o.m, k)
		}
	}
}

// saveLocked атомарно пишет файл 0600; вызывается под mu.
func (o *ownerSessions) saveLocked() error {
	raw, err := json.Marshal(o.m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(o.path), 0o700); err != nil {
		return err
	}
	tmp := o.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}

func (o *ownerSessions) set(token string, exp time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.pruneLocked(time.Now())
	o.m[sessionKey(token)] = exp
	return o.saveLocked()
}

func (o *ownerSessions) valid(token string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	exp, ok := o.m[sessionKey(token)]
	return ok && time.Now().Before(exp)
}

func (o *ownerSessions) delete(token string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.m, sessionKey(token))
	return o.saveLocked()
}

// localOwnerIDFor — ID владельца: строка прежней установки (тот же
// chats/u<ID>), иначе localOwnerID. Новых строк в users не создаёт.
func (s *Server) localOwnerIDFor(login string) int64 {
	if u, err := s.db.UserByLogin(login); err == nil {
		return u.ID
	}
	return localOwnerID
}

// useOwnerSessions переключает authN на сессии владельца: единственный
// пользователь — владелец, роль — localOwnerRole, без таблиц users/sessions.
func (s *Server) useOwnerSessions(ownerID int64, login string) {
	sessions := loadOwnerSessions(filepath.Join(s.cfg.Home, ownerSessionsFile))
	s.authN.SetSession = func(token string, userID int64, exp time.Time) error {
		if userID != ownerID {
			return fmt.Errorf("local: сессия только для владельца")
		}
		return sessions.set(token, exp)
	}
	s.authN.SessionUser = func(token string) (int64, string, string, error) {
		if !sessions.valid(token) {
			return 0, "", "", auth.ErrUnauthorized
		}
		return ownerID, login, localOwnerRole, nil
	}
	s.authN.DeleteSession = sessions.delete
}

// roleOf — роль пользователя: владелец локального режима — из конфигурации,
// остальные — из users; пусто, если пользователя нет.
func (s *Server) roleOf(userID int64) string {
	if s.local != nil && userID == s.local.ownerID {
		return localOwnerRole
	}
	u, err := s.db.UserByID(userID)
	if err != nil {
		return ""
	}
	return u.Role
}
