package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestDZ2LoginUserLogoutLifecycle — жизненный цикл сессии через User():
// логин создаёт сессию, User её видит, логин-аут уничтожает.
func TestDZ2LoginUserLogoutLifecycle(t *testing.T) {
	sessions := map[string]struct {
		id    int64
		login string
		role  string
	}{}
	s := &Service{
		SetSession: func(token string, id int64, exp time.Time) error {
			sessions[token] = struct {
				id    int64
				login string
				role  string
			}{id, "dz2_user", "user"}
			return nil
		},
		SessionUser: func(token string) (int64, string, string, error) {
			if v, ok := sessions[token]; ok {
				return v.id, v.login, v.role, nil
			}
			return 0, "", "", ErrUnauthorized
		},
		DeleteSession: func(token string) error {
			delete(sessions, token)
			return nil
		},
	}
	w := httptest.NewRecorder()
	// Внимание: Login передаёт в verify (password, passwordHash), хотя
	// сигнатура объявлена как (hash, pw) - известная нечитаемость контракта.
	verify := func(pw, hash string) (bool, error) {
		return pw == "pw" && hash == "hash", nil
	}
	if err := s.Login(w, 7, "hash", "pw", verify); err != nil {
		t.Fatalf("login: %v", err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	for _, c := range w.Result().Cookies() {
		req.AddCookie(c)
	}
	id, login, role, err := s.User(req)
	if err != nil || id != 7 || login != "dz2_user" || role != "user" {
		t.Fatalf("User: %v, %d, %q, %q", err, id, login, role)
	}
	s.Logout(w, req)
	if _, _, _, err := s.User(req); err != ErrUnauthorized {
		t.Fatalf("после logout User = %v, ждали ErrUnauthorized", err)
	}
}
