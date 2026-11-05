package store

// activity_test.go — агрегаты активности для графиков админ-панели:
// часовое/дневное группирование журнала шлюза, топ пользователей, статусы,
// регистрации по дням. Данные вставляются с явным ts (UTC «YYYY-MM-DD …»),
// как их пишет SQLite datetime('now').

import "testing"

func TestProxyRequestsActivity(t *testing.T) {
	db := openTest(t)
	type row struct {
		ts     string // SQLite-выражение времени (datetime('now', …))
		userID int64
		login  string
		status int
		ms     int64
		bytes  int64
	}
	bucket := func(query string) string {
		var s string
		if err := db.QueryRow("SELECT " + query).Scan(&s); err != nil {
			t.Fatalf("now: %v", err)
		}
		return s
	}
	curHour := bucket("strftime('%Y-%m-%dT%H:00:00Z', 'now')")
	curDay := bucket("strftime('%Y-%m-%dT00:00:00Z', 'now')")
	yestDay := bucket("strftime('%Y-%m-%dT00:00:00Z', 'now', '-1 day')")
	lastHour := bucket("strftime('%Y-%m-%dT%H:00:00Z', 'now', '-1 hour')")

	rows := []row{
		// текущий час: 2 запроса от anna, один из них — ошибка
		{"datetime('now', '-5 minutes')", 1, "anna", 200, 100, 500},
		{"datetime('now', '-2 minutes')", 1, "anna", 500, 200, 10},
		// прошлый час — от bob
		{"datetime('now', '-1 hour')", 2, "bob", 200, 50, 700},
		// вчера (в 24-часовой срез не попадает, в 7-дневный — да)
		{"datetime('now', '-1 day', '-3 hours')", 1, "anna", 404, 20, 0},
		// две недели назад — не попадает никуда
		{"datetime('now', '-20 days')", 1, "anna", 200, 10, 0},
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO proxy_requests(ts, user_id, login, method, path, status, ms, bytes_out)
			VALUES (`+r.ts+`, ?, ?, 'GET', '/chat/x', ?, ?, ?)`,
			r.userID, r.login, r.status, r.ms, r.bytes); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users(login, password_hash, role, created_at)
		VALUES ('anna', 'h', 'user', datetime('now'))`); err != nil {
		t.Fatalf("user insert: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users(login, password_hash, role, created_at)
		VALUES ('bob', 'h', 'user', datetime('now', '-1 day', '-3 hours'))`); err != nil {
		t.Fatalf("user insert: %v", err)
	}

	// hourly: непустые бакеты последних 24 часов
	hourly, err := db.ProxyRequestsHourly(24)
	if err != nil {
		t.Fatalf("hourly: %v", err)
	}
	found := map[string]ActivityPoint{}
	for _, p := range hourly {
		if p.Requests > 0 {
			found[p.Bucket] = p
		}
	}
	if found[curHour].Requests != 2 || found[curHour].Users != 1 || found[curHour].Errors != 1 {
		t.Fatalf("hourly текущий час: %+v (все: %v)", found[curHour], found)
	}
	if p := found[lastHour]; p.Requests != 1 || p.Users != 1 || p.Errors != 0 {
		t.Fatalf("hourly прошлый час: %+v", p)
	}
	// 27 часов назад в часовой срез (24ч) не попадает
	for _, p := range hourly {
		if p.Bucket == yestDay[:11]+"09:00:00" && p.Requests > 0 {
			t.Fatalf("вчерашний запрос попал в 24-часовой срез: %+v", p)
		}
	}

	// daily: суммарно 4 запроса в 14-дневном окне (5-я запись — 20 дней назад),
	// распределение по бакетам может сдвигаться у полуночи — проверяем сумму.
	daily, err := db.ProxyRequestsDaily(14)
	if err != nil {
		t.Fatalf("daily: %v", err)
	}
	var windowSum int64
	for _, p := range daily {
		windowSum += p.Requests
	}
	if windowSum != 4 {
		t.Fatalf("daily: сумма в окне = %d, ждём 4", windowSum)
	}

	// top users за 7 дней: anna 3 запроса, bob 1
	top, err := db.ProxyRequestsTopUsers(7, 10)
	if err != nil || len(top) != 2 {
		t.Fatalf("top: %v len=%d", err, len(top))
	}
	if top[0].Login != "anna" || top[0].Requests != 3 || top[0].Errors != 2 {
		t.Fatalf("top[0]: %+v", top[0])
	}
	if top[1].Login != "bob" || top[1].BytesOut != 700 {
		t.Fatalf("top[1]: %+v", top[1])
	}

	// статусы за 7 дней: 2×2xx (anna 200 + bob 200), 1×4xx, 1×5xx
	st, err := db.ProxyRequestsStatuses(7)
	if err != nil {
		t.Fatalf("statuses: %v", err)
	}
	if st.OK != 2 || st.ClientError != 1 || st.ServerError != 1 || st.Redirect != 0 {
		t.Fatalf("statuses: %+v", st)
	}

	// регистрации по дням
	reg, err := db.UsersRegisteredDaily(14)
	if err != nil {
		t.Fatalf("registrations: %v", err)
	}
	byDay := map[string]int64{}
	for _, r := range reg {
		if r.Count > 0 {
			byDay[r.Day] = r.Count
		}
	}
	if byDay[curDay] != 1 || byDay[yestDay] != 1 {
		t.Fatalf("registrations: %+v", byDay)
	}
}
