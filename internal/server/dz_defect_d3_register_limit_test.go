package server

// dz_defect_d3_register_limit_test.go — красно-зелёный цикл дефекта D3
// (ДЗ РТиППО): регистрация с одного адреса ограничивается частотным
// лимитом. Красная фаза зафиксирована 09.11.2026: 40 регистраций подряд
// проходили без единого 429 (протокол ручного прогона: 25 аккаунтов за 0,5 с).

import (
	"net/http"
	"testing"
)

// TestDZRegisterBurstIsRateLimited — серия регистраций с одного адреса
// должна упираться в лимит: часть запросов получает 429, а в базе
// остаётся не больше лимита учётных записей. Красная фаза: все 40 запросов
// проходят, лимита нет.
func TestDZRegisterBurstIsRateLimited(t *testing.T) {
	env := newTestEnv(t, 3)
	c := env.newClient()
	limited := 0
	for i := 0; i < 40; i++ {
		code, _ := env.do(c, "POST", "/api/register", map[string]string{
			"login":    "burst" + string(rune('a'+i/10)) + string(rune('0'+i%10)),
			"password": "secret1",
		})
		if code == http.StatusTooManyRequests {
			limited++
			continue
		}
		if code != 200 {
			t.Fatalf("регистрация %d: неожиданный код %d", i+1, code)
		}
	}
	users, err := env.db.UsersList()
	if err != nil {
		t.Fatal(err)
	}
	if limited == 0 {
		t.Fatalf("D3: 40 регистраций подряд с одного адреса — ни одного 429, ограничение частоты отсутствует (создано %d учёток)", len(users))
	}
	if len(users) > 31 { // 1 админ + 30 в пределах минутного лимита
		t.Fatalf("D3: создано %d учёток — лимит регистраций не сработал", len(users))
	}
}
