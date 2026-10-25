package store

import "fmt"

// roles.go — роль developer (v3 unified runtime): заявка → апрув админом →
// привязка к учётке (users.role = 'developer', постоянная).

type RoleRequest struct {
	ID         int64
	UserID     int64
	Login      string // заполняется только в RoleRequestsAll (JOIN users)
	Status     string // pending | approved | rejected
	Motivation string
	DecidedBy  string
	DecidedAt  string
	CreatedAt  string
}

func (d *DB) CreateRoleRequest(userID int64, motivation string) (int64, error) {
	res, err := d.Exec(`INSERT INTO role_requests(user_id, motivation) VALUES(?,?)`,
		userID, motivation)
	if err != nil {
		return 0, wrap("create role request", err)
	}
	return res.LastInsertId()
}

func (d *DB) RoleRequestByID(id int64) (RoleRequest, error) {
	r := RoleRequest{}
	err := d.QueryRow(`SELECT id, user_id, status, motivation, decided_by, decided_at, created_at
		FROM role_requests WHERE id = ?`, id).
		Scan(&r.ID, &r.UserID, &r.Status, &r.Motivation, &r.DecidedBy, &r.DecidedAt, &r.CreatedAt)
	return r, wrap("role request by id", err)
}

func (d *DB) RoleRequestsForUser(userID int64) ([]RoleRequest, error) {
	rows, err := d.Query(`SELECT id, user_id, status, motivation, decided_by, decided_at, created_at
		FROM role_requests WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, wrap("role requests by user", err)
	}
	defer rows.Close()
	var out []RoleRequest
	for rows.Next() {
		var r RoleRequest
		if err := rows.Scan(&r.ID, &r.UserID, &r.Status, &r.Motivation, &r.DecidedBy, &r.DecidedAt, &r.CreatedAt); err != nil {
			return nil, wrap("scan role request", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) RoleRequestsAll() ([]RoleRequest, error) {
	rows, err := d.Query(`SELECT rr.id, rr.user_id, COALESCE(u.login, ''), rr.status, rr.motivation,
		rr.decided_by, rr.decided_at, rr.created_at
		FROM role_requests rr LEFT JOIN users u ON u.id = rr.user_id
		ORDER BY rr.created_at DESC`)
	if err != nil {
		return nil, wrap("role requests all", err)
	}
	defer rows.Close()
	var out []RoleRequest
	for rows.Next() {
		var r RoleRequest
		if err := rows.Scan(&r.ID, &r.UserID, &r.Login, &r.Status, &r.Motivation, &r.DecidedBy, &r.DecidedAt, &r.CreatedAt); err != nil {
			return nil, wrap("scan role request", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingRoleRequestExists — есть ли у пользователя незакрытая заявка.
func (d *DB) PendingRoleRequestExists(userID int64) (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(1) FROM role_requests WHERE user_id = ? AND status = 'pending'`, userID).Scan(&n)
	return n > 0, wrap("pending role request", err)
}

// DecideRoleRequest закрывает pending-заявку (idempotent-гвардия через
// WHERE status='pending') и возвращает user_id заявки для смены роли.
func (d *DB) DecideRoleRequest(id int64, status, decidedBy string) (int64, error) {
	res, err := d.Exec(`UPDATE role_requests SET status = ?, decided_by = ?, decided_at = ?, updated_at = ?
		WHERE id = ? AND status = 'pending'`, status, decidedBy, now(), now(), id)
	if err != nil {
		return 0, wrap("decide role request", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, fmt.Errorf("store: заявка %d уже решена или не существует", id)
	}
	var userID int64
	err = d.QueryRow(`SELECT user_id FROM role_requests WHERE id = ?`, id).Scan(&userID)
	return userID, wrap("role request user", err)
}

func (d *DB) UpdateUserRole(userID int64, role string) error {
	_, err := d.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, userID)
	return wrap("update user role", err)
}

// LoginOrID — строковое представление заявителя для аудита (логин, если
// заполнен при JOIN-выборке, иначе числовой id).
func (r RoleRequest) LoginOrID(fallback int64) string {
	if r.Login != "" {
		return r.Login
	}
	return fmt.Sprintf("%d", fallback)
}
