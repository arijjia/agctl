package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound — универсальная «нет записи».
var ErrNotFound = errors.New("store: not found")

// User — учётная запись.
type User struct {
	ID           int64
	Login        string
	PasswordHash string
	Role         string // user | admin
	CreatedAt    string
}

// Workspace — «контейнер + репка».
type Workspace struct {
	ID          string
	OwnerID     int64
	Name        string
	Template    string // base | 1c-vibe | ...
	Status      string // provisioning | running | stopped | failed | removing
	RepoPath    string
	WorkPath    string
	DshHome     string
	ContainerID string
	HostPort    int
	BaseURL     string
	Model       string   // модель по умолчанию (legacy-колонка)
	Models      []string // весь список (колонка models, "\n"-разделитель)
	Error       string
	CreatedAt   string
	UpdatedAt   string
	Owner       string // login, заполняется при join
}

// Secret — сохранённые настройки провайдера пользователя (токен — шифротекст).
type Secret struct {
	UserID     int64
	BaseURL    string
	Model      string
	Models     []string
	Ciphertext string
	UpdatedAt  string
}

// EncodeModels — список моделей в TEXT-колонку.
func EncodeModels(models []string) string {
	var clean []string
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" {
			clean = append(clean, m)
		}
	}
	return strings.Join(clean, "\n")
}

// DecodeModels — обратно; пусто + fallback = одна модель.
func DecodeModels(raw, fallback string) []string {
	var out []string
	for _, m := range strings.Split(raw, "\n") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	if len(out) == 0 && fallback != "" {
		out = []string{fallback}
	}
	return out
}

// AuditEvent — запись журнала безопасности (РСБ).
type AuditEvent struct {
	ID      int64
	TS      string
	Actor   string
	Action  string
	Target  string
	Details string
}

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("store: %s: %w", op, err)
}

// ── users ────────────────────────────────────────────────────────────────────

func (d *DB) CreateUser(login, passwordHash, role string) (int64, error) {
	res, err := d.Exec(`INSERT INTO users(login, password_hash, role) VALUES(?,?,?)`,
		login, passwordHash, role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, fmt.Errorf("store: login занят: %w", err)
		}
		return 0, wrap("create user", err)
	}
	return res.LastInsertId()
}

func (d *DB) CountUsers() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, wrap("count users", err)
}

func scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Login, &u.PasswordHash, &u.Role, &u.CreatedAt)
	return u, err
}

func (d *DB) UserByLogin(login string) (User, error) {
	u, err := scanUser(d.QueryRow(
		`SELECT id, login, password_hash, role, created_at FROM users WHERE login = ?`, login))
	return u, wrap("user by login", err)
}

func (d *DB) UserByID(id int64) (User, error) {
	u, err := scanUser(d.QueryRow(
		`SELECT id, login, password_hash, role, created_at FROM users WHERE id = ?`, id))
	return u, wrap("user by id", err)
}

func (d *DB) UsersList() ([]User, error) {
	rows, err := d.Query(`SELECT id, login, password_hash, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, wrap("users list", err)
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Login, &u.PasswordHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, wrap("users list scan", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ── sessions ─────────────────────────────────────────────────────────────────

func (d *DB) CreateSession(token string, userID int64, expires time.Time) error {
	_, err := d.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`,
		token, userID, expires.UTC().Format("2006-01-02 15:04:05"))
	return wrap("create session", err)
}

func (d *DB) SessionUser(token string) (User, error) {
	u, err := scanUser(d.QueryRow(`
		SELECT u.id, u.login, u.password_hash, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > datetime('now')`, token))
	return u, wrap("session user", err)
}

func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return wrap("delete session", err)
}

func (d *DB) DeleteExpiredSessions() error {
	_, err := d.Exec(`DELETE FROM sessions WHERE expires_at <= datetime('now')`)
	return wrap("delete expired sessions", err)
}

// ── workspaces ───────────────────────────────────────────────────────────────

const wsCols = `id, owner_id, name, template, status, repo_path, work_path, dsh_home,
	container_id, host_port, base_url, model, models, error, created_at, updated_at`

func scanWS(row interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	var modelsRaw string
	err := row.Scan(&w.ID, &w.OwnerID, &w.Name, &w.Template, &w.Status, &w.RepoPath, &w.WorkPath,
		&w.DshHome, &w.ContainerID, &w.HostPort, &w.BaseURL, &w.Model, &modelsRaw, &w.Error,
		&w.CreatedAt, &w.UpdatedAt)
	if err == nil {
		w.Models = DecodeModels(modelsRaw, w.Model)
	}
	return w, err
}

func (d *DB) InsertWorkspace(w Workspace) error {
	_, err := d.Exec(`INSERT INTO workspaces(id, owner_id, name, template, status, repo_path,
		work_path, dsh_home, container_id, host_port, base_url, model, models, error)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.OwnerID, w.Name, w.Template, w.Status, w.RepoPath, w.WorkPath, w.DshHome,
		w.ContainerID, w.HostPort, w.BaseURL, w.Model, EncodeModels(w.Models), w.Error)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("store: имя воркспейса занято: %w", err)
		}
		return wrap("insert workspace", err)
	}
	return nil
}

func (d *DB) WorkspaceByID(id string) (Workspace, error) {
	w, err := scanWS(d.QueryRow(`SELECT `+wsCols+` FROM workspaces WHERE id = ?`, id))
	return w, wrap("workspace by id", err)
}

func (d *DB) WorkspacesByOwner(ownerID int64) ([]Workspace, error) {
	return d.wsList(`SELECT `+wsCols+` FROM workspaces WHERE owner_id = ? ORDER BY created_at DESC`, ownerID)
}

func (d *DB) WorkspacesAll() ([]Workspace, error) {
	return d.wsList(`SELECT w.id, w.owner_id, w.name, w.template, w.status, w.repo_path, w.work_path,
		w.dsh_home, w.container_id, w.host_port, w.base_url, w.model, w.models, w.error,
		w.created_at, w.updated_at
		FROM workspaces w JOIN users u ON u.id = w.owner_id
		ORDER BY w.created_at DESC`)
}

func (d *DB) wsList(q string, args ...any) ([]Workspace, error) {
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, wrap("workspaces list", err)
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		w, err := scanWS(rows)
		if err != nil {
			return nil, wrap("workspaces scan", err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WorkspaceOwnerLogin — логин владельца (для списков админки).
func (d *DB) WorkspaceOwnerLogin(ownerID int64) string {
	var login string
	_ = d.QueryRow(`SELECT login FROM users WHERE id = ?`, ownerID).Scan(&login)
	return login
}

func (d *DB) UpdateWorkspaceStatus(id, status, errText string) error {
	_, err := d.Exec(`UPDATE workspaces SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, errText, now(), id)
	return wrap("update ws status", err)
}

func (d *DB) UpdateWorkspaceRuntime(id, containerID string, hostPort int, status string) error {
	_, err := d.Exec(`UPDATE workspaces SET container_id = ?, host_port = ?, status = ?,
		error = '', updated_at = ? WHERE id = ?`, containerID, hostPort, status, now(), id)
	return wrap("update ws runtime", err)
}

func (d *DB) UpdateWorkspacePaths(id, repoPath, workPath, dshHome string) error {
	_, err := d.Exec(`UPDATE workspaces SET repo_path = ?, work_path = ?, dsh_home = ?,
		updated_at = ? WHERE id = ?`, repoPath, workPath, dshHome, now(), id)
	return wrap("update ws paths", err)
}

func (d *DB) DeleteWorkspace(id string) error {
	_, err := d.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
	return wrap("delete workspace", err)
}

// ── secrets (LLM-токены, шифротекст) ────────────────────────────────────────

func (d *DB) SecretUpsert(userID int64, baseURL, model string, models []string, ciphertext string) error {
	_, err := d.Exec(`INSERT INTO secrets(user_id, base_url, model, models, ciphertext, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET base_url = excluded.base_url,
		  model = excluded.model, models = excluded.models,
		  ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`,
		userID, baseURL, model, EncodeModels(models), ciphertext, now())
	return wrap("secret upsert", err)
}

func (d *DB) SecretByUser(userID int64) (Secret, error) {
	var s Secret
	var modelsRaw string
	err := d.QueryRow(`SELECT user_id, base_url, model, models, ciphertext, updated_at
		FROM secrets WHERE user_id = ?`, userID).
		Scan(&s.UserID, &s.BaseURL, &s.Model, &modelsRaw, &s.Ciphertext, &s.UpdatedAt)
	if err == nil {
		s.Models = DecodeModels(modelsRaw, s.Model)
	}
	return s, wrap("secret by user", err)
}

// UpdateWorkspaceProvider — смена Base URL/моделей воркспейса (ротация).
func (d *DB) UpdateWorkspaceProvider(id, baseURL, model string, models []string) error {
	_, err := d.Exec(`UPDATE workspaces SET base_url = ?, model = ?, models = ?,
		updated_at = ? WHERE id = ?`, baseURL, model, EncodeModels(models), now(), id)
	return wrap("update ws provider", err)
}

// ── audit_log (РСБ) ─────────────────────────────────────────────────────────

func (d *DB) Audit(actor, action, target, details string) error {
	_, err := d.Exec(`INSERT INTO audit_log(actor, action, target, details) VALUES(?,?,?,?)`,
		actor, action, target, details)
	return wrap("audit", err)
}

func (d *DB) AuditList(limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := d.Query(`SELECT id, ts, actor, action, target, details
		FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrap("audit list", err)
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.ID, &e.TS, &e.Actor, &e.Action, &e.Target, &e.Details); err != nil {
			return nil, wrap("audit scan", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
