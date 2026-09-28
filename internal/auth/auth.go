// Package auth — пароли argon2id, cookie-сессии, базовый rate-limit логина.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	sessionCookie = "agctl_session"
	sessionTTL    = 7 * 24 * time.Hour
	// argon2id параметры (OWASP-базис: 64MB, t=1, p=4)
	argonMemory  uint32 = 64 * 1024
	argonTime    uint32 = 1
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// Service — сессии в store через колбэки (чтобы не тянуть store сюда).
type Service struct {
	SetSession     func(token string, userID int64, expires time.Time) error
	SessionUser    func(token string) (int64, string, string, error) // id, login, role
	DeleteSession  func(token string) error
	CookieSecure   bool
	failed         sync.Map // login → счётчик неудач
}

// ErrUnauthorized — нет/просрочена сессия.
var ErrUnauthorized = errors.New("auth: не авторизован")

// HashPassword → PHC-строка argon2id.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword — constant-time сверка.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false, fmt.Errorf("auth: неизвестный формат хеша")
	}
	var version int
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[1], "v=%d", &version); err != nil {
		return false, err
	}
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login проверяет пароль (verify(password, encodedHash)) и создаёт сессию.
func (s *Service) Login(w http.ResponseWriter, userID int64, passwordHash, password string, verify func(hash, pw string) (bool, error)) error {
	ok, err := verify(password, passwordHash)
	if err != nil || !ok {
		return fmt.Errorf("auth: неверный логин или пароль")
	}
	token, err := newToken()
	if err != nil {
		return err
	}
	exp := time.Now().Add(sessionTTL)
	if err := s.SetSession(token, userID, exp); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.CookieSecure, Expires: exp,
	})
	return nil
}

// Logout удаляет сессию и сбрасывает cookie.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.DeleteSession(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

// User — текущий пользователь запроса (id, login, role) или ErrUnauthorized.
func (s *Service) User(r *http.Request) (int64, string, string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return 0, "", "", ErrUnauthorized
	}
	return s.SessionUser(c.Value)
}

// ── rate-limit логина (в памяти, для MVP достаточно) ────────────────────────

type attempt struct {
	n   int
	until time.Time
}

const maxFails = 10
const lockDuration = 5 * time.Minute

// AllowLogin — true, если логину можно пытаться войти.
func (s *Service) AllowLogin(login string) bool {
	v, ok := s.failed.Load(login)
	if !ok {
		return true
	}
	a := v.(attempt)
	if time.Now().After(a.until) {
		s.failed.Delete(login)
		return true
	}
	return a.n < maxFails
}

// FailLogin фиксирует неудачную попытку.
func (s *Service) FailLogin(login string) {
	v, ok := s.failed.Load(login)
	n := 1
	if ok {
		n = v.(attempt).n + 1
	}
	s.failed.Store(login, attempt{n: n, until: time.Now().Add(lockDuration)})
}

// ResetLogin сбрасывает счётчик после успеха.
func (s *Service) ResetLogin(login string) { s.failed.Delete(login) }
