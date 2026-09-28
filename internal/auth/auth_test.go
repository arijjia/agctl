package auth

import "testing"

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
