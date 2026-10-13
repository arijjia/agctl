package auth

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestLoginSetsSecureCookieWhenConfigured(t *testing.T) {
	s := &Service{
		CookieSecure:  true,
		SetSession:    func(string, int64, time.Time) error { return nil },
		DeleteSession: func(string) error { return nil },
	}
	rr := httptest.NewRecorder()
	if err := s.Login(rr, 1, "hash", "pass", func(password, hash string) (bool, error) {
		return password == "pass" && hash == "hash", nil
	}); err != nil {
		t.Fatalf("login: %v", err)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("cookie flags: %+v", cookies)
	}
	logoutReq := httptest.NewRequest("POST", "/api/logout", nil)
	logoutReq.AddCookie(cookies[0])
	logout := httptest.NewRecorder()
	s.Logout(logout, logoutReq)
	cleared := logout.Result().Cookies()
	if len(cleared) != 1 || !cleared[0].Secure || cleared[0].MaxAge != -1 {
		t.Fatalf("logout cookie flags: %+v", cleared)
	}
}

func TestPasswordHashVerify(t *testing.T) {
	h, err := HashPassword("пароль-123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if ok, err := VerifyPassword("пароль-123", h); err != nil || !ok {
		t.Fatalf("верный пароль не прошёл: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword("другой", h); err != nil || ok {
		t.Fatalf("неверный пароль прошёл: ok=%v err=%v", ok, err)
	}
	if _, err := VerifyPassword("x", "мусор"); err == nil {
		t.Fatal("битый хеш должен давать ошибку")
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := &Service{}
	login := "anna"
	for i := 0; i < maxFails; i++ {
		if !s.AllowLogin(login) {
			t.Fatalf("попытка %d не должна блокироваться", i+1)
		}
		s.FailLogin(login)
	}
	if s.AllowLogin(login) {
		t.Fatal("после maxFails логин должен блокироваться")
	}
	s.ResetLogin(login)
	if !s.AllowLogin(login) {
		t.Fatal("после reset логин должен работать")
	}
}
