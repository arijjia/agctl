package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// admin.go — данные админки супер-админа (карточка admin-panel-super-admin):
// журнал проксированных запросов (proxy_requests) и квоты токенов (quotas).
// LiteLLM-ключ на платформу один, поэтому бюджеты назначает agctl; инжект
// виртуальных ключей LiteLLM — отдельная итерация, см. карточку.

// ProxyRequest — одна запись журнала шлюза /chat/.
type ProxyRequest struct {
	ID       int64  `json:"id"`
	TS       string `json:"ts"`
	UserID   int64  `json:"userId"`
	Login    string `json:"login"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Status   int    `json:"status"`
	MS       int64  `json:"ms"`
	BytesOut int64  `json:"bytesOut"`
}

// Quota — бюджет токенов на роль ('role') или пользователя ('user').
// Ноль = «без лимита на этом уровне»; пользовательский уровень перекрывает
// ролевой. Ежедневный бюджет обнуляется каждый день, месячный — в начале
// месяца (интерпретация на стороне enforcement-итерации).
type Quota struct {
	ID            int64  `json:"id"`
	Scope         string `json:"scope"`   // role | user
	Subject       string `json:"subject"` // роль или login пользователя
	DailyTokens   int64  `json:"dailyTokens"`
	MonthlyTokens int64  `json:"monthlyTokens"`
	Note          string `json:"note"`
	UpdatedAt     string `json:"updatedAt"`
}

// ProxyRequestInsert — дописать запись журнала шлюза.
func (d *DB) ProxyRequestInsert(userID int64, login, method, path string, status int, ms int64, bytesOut int64) error {
	_, err := d.Exec(`INSERT INTO proxy_requests(user_id, login, method, path, status, ms, bytes_out)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, login, method, path, status, ms, bytesOut)
	if err != nil {
		return wrap("proxy request insert", err)
	}
	return nil
}

// ProxyRequestsList — свежие записи журнала, опционально по пользователю.
func (d *DB) ProxyRequestsList(userID int64, limit int) ([]ProxyRequest, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT id, ts, user_id, login, method, path, status, ms, bytes_out
		FROM proxy_requests`
	args := []any{}
	if userID > 0 {
		q += ` WHERE user_id = ?`
		args = append(args, userID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, wrap("proxy requests list", err)
	}
	defer rows.Close()
	out := []ProxyRequest{}
	for rows.Next() {
		var r ProxyRequest
		if err := rows.Scan(&r.ID, &r.TS, &r.UserID, &r.Login, &r.Method, &r.Path, &r.Status, &r.MS, &r.BytesOut); err != nil {
			return nil, wrap("proxy request scan", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProxyRequestsPrune — держим журнал ограниченным: удаляем всё, кроме
// последних keep строк. Возвращает число удалённых.
func (d *DB) ProxyRequestsPrune(keep int) (int64, error) {
	if keep <= 0 {
		keep = 5000
	}
	res, err := d.Exec(`DELETE FROM proxy_requests WHERE id <= (SELECT MAX(id) - ? FROM proxy_requests)`, keep)
	if err != nil {
		return 0, wrap("proxy requests prune", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// SessionsCount — активных (не истёкших) сессий в таблице sessions.
func (d *DB) SessionsCount() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM sessions WHERE expires_at > datetime('now')`).Scan(&n)
	return n, wrap("sessions count", err)
}

// ProxyRequestsCountToday — записей журнала шлюза за сегодня (UTC).
func (d *DB) ProxyRequestsCountToday() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM proxy_requests WHERE ts >= date('now')`).Scan(&n)
	return n, wrap("proxy requests count today", err)
}

// QuotaUpsert — создать или обновить квоту по паре (scope, subject).
func (d *DB) QuotaUpsert(scope, subject string, daily, monthly int64, note string) error {
	if scope != "role" && scope != "user" {
		return fmt.Errorf("store: quota scope %q: допустимы role|user", scope)
	}
	if subject == "" {
		return fmt.Errorf("store: quota subject пуст")
	}
	_, err := d.Exec(`INSERT INTO quotas(scope, subject, daily_tokens, monthly_tokens, note, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(scope, subject) DO UPDATE SET
			daily_tokens = excluded.daily_tokens,
			monthly_tokens = excluded.monthly_tokens,
			note = excluded.note,
			updated_at = excluded.updated_at`,
		scope, subject, daily, monthly, note, now())
	if err != nil {
		return wrap("quota upsert", err)
	}
	return nil
}

// QuotaDelete — удалить квоту по id.
func (d *DB) QuotaDelete(id int64) error {
	_, err := d.Exec(`DELETE FROM quotas WHERE id = ?`, id)
	if err != nil {
		return wrap("quota delete", err)
	}
	return nil
}

// QuotaList — все квоты.
func (d *DB) QuotaList() ([]Quota, error) {
	rows, err := d.Query(`SELECT id, scope, subject, daily_tokens, monthly_tokens, note, updated_at
		FROM quotas ORDER BY scope, subject`)
	if err != nil {
		return nil, wrap("quota list", err)
	}
	defer rows.Close()
	out := []Quota{}
	for rows.Next() {
		var q Quota
		if err := rows.Scan(&q.ID, &q.Scope, &q.Subject, &q.DailyTokens, &q.MonthlyTokens, &q.Note, &q.UpdatedAt); err != nil {
			return nil, wrap("quota scan", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// QuotaEffectiveForUser — действующая квота пользователя: персональная
// запись перекрывает ролевую. Пустая Quota{} = лимит не задан.
func (d *DB) QuotaEffectiveForUser(role, login string) (Quota, error) {
	row := d.QueryRow(`SELECT id, scope, subject, daily_tokens, monthly_tokens, note, updated_at
		FROM quotas WHERE (scope = 'user' AND subject = ?) OR (scope = 'role' AND subject = ?)
		ORDER BY CASE scope WHEN 'user' THEN 0 ELSE 1 END LIMIT 1`, login, role)
	var q Quota
	err := row.Scan(&q.ID, &q.Scope, &q.Subject, &q.DailyTokens, &q.MonthlyTokens, &q.Note, &q.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Quota{}, nil
	}
	if err != nil {
		return Quota{}, wrap("quota effective", err)
	}
	return q, nil
}
