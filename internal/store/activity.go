package store

import (
	"fmt"
)

// activity.go — временные ряды активности для графиков админ-панели
// (вкладка «Обзор»): журнал шлюза proxy_requests, агрегированный по часам
// и дням, топ пользователей, статусы ответов и регистрации учёток.
// Все бакеты считаются SQLite (UTC-данные), сервер только досыпает пустые.

// ActivityPoint — одна точка временного ряда: бакет + агрегаты журнала шлюза.
type ActivityPoint struct {
	Bucket   string  `json:"t"` // "2026-11-05T13:00:00Z" / "2026-11-05T00:00:00Z"
	Requests int64   `json:"requests"`
	Users    int64   `json:"users"`
	Errors   int64   `json:"errors"` // статус ≥ 400
	AvgMS    float64 `json:"avgMs"`
}

// ActivityUser — агрегат по пользователю (топ активности).
type ActivityUser struct {
	Login    string  `json:"login"`
	Requests int64   `json:"requests"`
	Errors   int64   `json:"errors"`
	AvgMS    float64 `json:"avgMs"`
	BytesOut int64   `json:"bytesOut"`
}

// ActivityStatuses — разбивка ответов журнала по классам статуса.
type ActivityStatuses struct {
	OK          int64 `json:"ok"`          // 2xx
	Redirect    int64 `json:"redirect"`    // 3xx
	ClientError int64 `json:"clientError"` // 4xx
	ServerError int64 `json:"serverError"` // 5xx и прочее
}

// DayCount — счётчик по дню (регистрации учёток).
type DayCount struct {
	Day   string `json:"t"` // "2026-11-05T00:00:00Z"
	Count int64  `json:"count"`
}

// scanActivity — общий скан строк агрегата журнала.
func (d *DB) scanActivity(q string, args []any) ([]ActivityPoint, error) {
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, wrap("activity query", err)
	}
	defer rows.Close()
	out := []ActivityPoint{}
	for rows.Next() {
		var p ActivityPoint
		if err := rows.Scan(&p.Bucket, &p.Requests, &p.Users, &p.Errors, &p.AvgMS); err != nil {
			return nil, wrap("activity scan", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProxyRequestsHourly — журнал по часам за последние hours часов (UTC).
func (d *DB) ProxyRequestsHourly(hours int) ([]ActivityPoint, error) {
	if hours <= 0 || hours > 24*30 {
		hours = 24
	}
	return d.scanActivity(`SELECT strftime('%Y-%m-%dT%H:00:00Z', ts) AS bucket,
			COUNT(*), COUNT(DISTINCT user_id),
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END),
			AVG(ms)
		FROM proxy_requests
		WHERE ts >= datetime('now', ?)
		GROUP BY bucket ORDER BY bucket`,
		[]any{fmt.Sprintf("-%d hours", hours-1)})
}

// ProxyRequestsDaily — журнал по дням за последние days дней (UTC).
func (d *DB) ProxyRequestsDaily(days int) ([]ActivityPoint, error) {
	if days <= 0 || days > 365 {
		days = 14
	}
	return d.scanActivity(`SELECT strftime('%Y-%m-%dT00:00:00Z', ts) AS bucket,
			COUNT(*), COUNT(DISTINCT user_id),
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END),
			AVG(ms)
		FROM proxy_requests
		WHERE ts >= datetime('now', ?)
		GROUP BY bucket ORDER BY bucket`,
		[]any{fmt.Sprintf("-%d days", days)})
}

// ProxyRequestsTopUsers — самые активные пользователи за последние days дней.
func (d *DB) ProxyRequestsTopUsers(days, limit int) ([]ActivityUser, error) {
	if days <= 0 {
		days = 7
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := d.Query(`SELECT login, COUNT(*),
			SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END),
			AVG(ms), SUM(bytes_out)
		FROM proxy_requests
		WHERE ts >= datetime('now', ?)
		GROUP BY user_id, login
		ORDER BY COUNT(*) DESC LIMIT ?`,
		fmt.Sprintf("-%d days", days), limit)
	if err != nil {
		return nil, wrap("activity top users", err)
	}
	defer rows.Close()
	out := []ActivityUser{}
	for rows.Next() {
		var u ActivityUser
		if err := rows.Scan(&u.Login, &u.Requests, &u.Errors, &u.AvgMS, &u.BytesOut); err != nil {
			return nil, wrap("activity top user scan", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ProxyRequestsStatuses — классы статусов журнала за последние days дней.
func (d *DB) ProxyRequestsStatuses(days int) (ActivityStatuses, error) {
	if days <= 0 {
		days = 7
	}
	var st ActivityStatuses
	// COALESCE: пустой журнал даёт NULL от SUM, а не ноль.
	err := d.QueryRow(`SELECT
			COALESCE(SUM(CASE WHEN status >= 200 AND status < 300 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status >= 300 AND status < 400 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status >= 400 AND status < 500 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status >= 500 OR status = 0 THEN 1 ELSE 0 END), 0)
		FROM proxy_requests WHERE ts >= datetime('now', ?)`,
		fmt.Sprintf("-%d days", days)).Scan(&st.OK, &st.Redirect, &st.ClientError, &st.ServerError)
	if err != nil {
		return ActivityStatuses{}, wrap("activity statuses", err)
	}
	return st, nil
}

// UsersRegisteredDaily — регистрации учёток по дням за последние days дней.
func (d *DB) UsersRegisteredDaily(days int) ([]DayCount, error) {
	if days <= 0 || days > 365 {
		days = 14
	}
	rows, err := d.Query(`SELECT strftime('%Y-%m-%dT00:00:00Z', created_at) AS day, COUNT(*)
		FROM users
		WHERE created_at >= date('now', ?)
		GROUP BY day ORDER BY day`,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return nil, wrap("users registered daily", err)
	}
	defer rows.Close()
	out := []DayCount{}
	for rows.Next() {
		var dc DayCount
		if err := rows.Scan(&dc.Day, &dc.Count); err != nil {
			return nil, wrap("users registered scan", err)
		}
		out = append(out, dc)
	}
	return out, rows.Err()
}

// RecentChatUsers — userID самых недавних активных пользователей по журналу
// шлюза (MAX(ts) на пользователя): топливо prewarm-пула чатов. Только
// существующие учётки; пустой журнал — пустой список.
func (d *DB) RecentChatUsers(limit int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := d.Query(`SELECT p.user_id
		FROM proxy_requests p JOIN users u ON u.id = p.user_id
		GROUP BY p.user_id ORDER BY MAX(p.ts) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrap("recent chat users", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, wrap("recent chat user scan", err)
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
