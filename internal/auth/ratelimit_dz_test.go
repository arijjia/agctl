package auth

// ratelimit_dz_test.go — юнит-проверка лимитера регистраций (дефект D3,
// ДЗ РТиППО): скользящее окно, независимость адресов и освобождение окна.

import (
	"testing"
	"time"
)

func TestDZRegisterLimiterSlidingWindow(t *testing.T) {
	now := time.Unix(1700000000, 0)
	l := NewRegisterLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("10.0.0.1"); !ok {
			t.Fatalf("попытка %d отклонена при лимите 3", i+1)
		}
	}
	ok, retryAfter := l.Allow("10.0.0.1")
	if ok || retryAfter <= 0 {
		t.Fatalf("4-я попытка прошла либо Retry-After пустой (ok=%v, retryAfter=%v)", ok, retryAfter)
	}
	if ok, _ := l.Allow("10.0.0.2"); !ok {
		t.Fatal("чужой адрес попал под лимит первого")
	}
	now = now.Add(61 * time.Second) // окно истекло
	if ok, _ := l.Allow("10.0.0.1"); !ok {
		t.Fatal("окно не освободилось после минуты")
	}
}
