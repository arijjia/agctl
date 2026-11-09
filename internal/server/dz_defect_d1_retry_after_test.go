package server

// dz_defect_d1_retry_after_test.go — красно-зелёный цикл дефекта D1
// (ДЗ РТиППО): ответ 429 при блокировке входа обязан сообщать остаток
// времени — заголовком Retry-After и в тексте. Красная фаза зафиксирована
// 09.11.2026: сообщение было статическим («подождите 5 минут»), заголовка нет.

import (
	"net/http"
	"strings"
	"testing"
)

// TestDZLoginLockoutCarriesRetryAfter — после 10 неудачных попыток вход
// блокируется, и ответ обязан сообщать, КОГДА блокировка снимется:
// заголовок Retry-After и остаток времени в тексте. Красная фаза:
// сообщение статическое, заголовка нет.
func TestDZLoginLockoutCarriesRetryAfter(t *testing.T) {
	env := newTestEnv(t, 3)
	env.register(env.admin, "anna", "secret1")
	c := env.newClient()
	for i := 0; i < 10; i++ {
		if code, _ := env.do(c, "POST", "/api/login", map[string]string{"login": "anna", "password": "wrong-password"}); code != 401 {
			t.Fatalf("неудачная попытка %d: код %d, ждём 401", i+1, code)
		}
	}
	req, err := http.NewRequest("POST", env.api.URL+"/api/login", strings.NewReader(`{"login":"anna","password":"secret1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	for {
		n, _ := resp.Body.Read(tmp)
		if n == 0 {
			break
		}
		buf = append(buf, tmp[:n]...)
	}
	body := string(buf)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("верный пароль при блокировке: код %d, ждём 429", resp.StatusCode)
	}
	retry := resp.Header.Get("Retry-After")
	if retry == "" {
		t.Fatalf("D1: 429 без заголовка Retry-After (тело %q) — клиент не знает, когда блокировка снимется", body)
	}
	if !strings.Contains(body, "осталось") {
		t.Fatalf("D1: сообщение блокировки без остатка времени: %q", body)
	}
}
