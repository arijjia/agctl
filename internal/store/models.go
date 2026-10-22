package store

import (
	"database/sql"
	"encoding/json"
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
	Template    string   // kit id (legacy column name)
	Components  []string // выбранный версионированный состав комплекта
	Status      string   // provisioning | running | stopped | failed | removing
	RepoPath    string
	WorkPath    string
	DshHome     string
	ContainerID string
	HostPort    int
	BaseURL     string   // primary/legacy URL (первый элемент BaseURLs)
	BaseURLs    []string // список endpoints; сериализуется в workspaces.base_urls
	Model       string   // модель по умолчанию (legacy-колонка)
	Models      []string // весь список (колонка models, "\n"-разделитель)
	Error       string
	SSHPubKey   string // публичный ключ для контейнерного sshd (пусто = sshd выключен)
	SSHPort     int    // опубликованный на 127.0.0.1 порт sshd (0 = не опубликован)
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

// EncodeBaseURLs stores URL lists as JSON so commas/newlines in opaque paths
// never change their boundaries. URLs are de-duplicated in input order.
func EncodeBaseURLs(urls []string) string {
	clean := normalizeBaseURLs(urls)
	b, _ := json.Marshal(clean)
	return string(b)
}

// DecodeBaseURLs reads the multi-URL column, falling back to the legacy scalar.
func DecodeBaseURLs(raw, fallback string) []string {
	var urls []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &urls)
	}
	urls = normalizeBaseURLs(urls)
	if len(urls) == 0 && strings.TrimSpace(fallback) != "" {
		urls = []string{strings.TrimSpace(fallback)}
	}
	return urls
}

func normalizeBaseURLs(urls []string) []string {
	seen := make(map[string]bool, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		u := strings.TrimSpace(raw)
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// EncodeIDs/DecodeIDs — детерминированное хранение списка component IDs.
func EncodeIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func DecodeIDs(raw string) []string {
	if raw == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
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
	container_id, host_port, base_url, base_urls, model, models, components, error, created_at, updated_at,
	ssh_pub_key, ssh_port`

func scanWS(row interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	var baseURLsRaw, modelsRaw, componentsRaw string
	err := row.Scan(&w.ID, &w.OwnerID, &w.Name, &w.Template, &w.Status, &w.RepoPath, &w.WorkPath,
		&w.DshHome, &w.ContainerID, &w.HostPort, &w.BaseURL, &baseURLsRaw, &w.Model, &modelsRaw, &componentsRaw, &w.Error,
		&w.CreatedAt, &w.UpdatedAt, &w.SSHPubKey, &w.SSHPort)
	if err == nil {
		w.BaseURLs = DecodeBaseURLs(baseURLsRaw, w.BaseURL)
		if len(w.BaseURLs) > 0 {
			w.BaseURL = w.BaseURLs[0]
		}
		w.Models = DecodeModels(modelsRaw, w.Model)
		w.Components = DecodeIDs(componentsRaw)
	}
	return w, err
}

func (d *DB) InsertWorkspace(w Workspace) error {
	baseURLs := w.BaseURLs
	if len(baseURLs) == 0 && w.BaseURL != "" {
		baseURLs = []string{w.BaseURL}
	}
	_, err := d.Exec(`INSERT INTO workspaces(id, owner_id, name, template, status, repo_path,
		work_path, dsh_home, container_id, host_port, base_url, base_urls, model, models, components, error, ssh_pub_key, ssh_port)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.OwnerID, w.Name, w.Template, w.Status, w.RepoPath, w.WorkPath, w.DshHome,
		w.ContainerID, w.HostPort, w.BaseURL, EncodeBaseURLs(baseURLs), w.Model, EncodeModels(w.Models), EncodeIDs(w.Components), w.Error,
		w.SSHPubKey, w.SSHPort)
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
		w.dsh_home, w.container_id, w.host_port, w.base_url, w.base_urls, w.model, w.models, w.components, w.error,
		w.created_at, w.updated_at, w.ssh_pub_key, w.ssh_port
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

// UpdateWorkspaceSSH — ключ и опубликованный порт sshd (пустой ключ = выключен).
func (d *DB) UpdateWorkspaceSSH(id, pubKey string, port int) error {
	_, err := d.Exec(`UPDATE workspaces SET ssh_pub_key = ?, ssh_port = ?, updated_at = ? WHERE id = ?`,
		pubKey, port, now(), id)
	return wrap("update ws ssh", err)
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

// ── local connector credentials ( durable pairing после one-time grant ) ────

// LocalConnector — durable credential ноутбука: хранится только хеш токена.
type LocalConnector struct {
	ID         int64
	UserID     int64
	TokenHash  string
	Label      string
	CreatedAt  string
	LastSeenAt string
}

func (d *DB) LocalConnectorCreate(userID int64, tokenHash, label string) (int64, error) {
	res, err := d.Exec(`INSERT INTO local_connectors(user_id, token_hash, label, created_at, last_seen_at)
		VALUES(?,?,?,?,?)`, userID, tokenHash, label, now(), now())
	if err != nil {
		return 0, wrap("local connector create", err)
	}
	id, err := res.LastInsertId()
	return id, wrap("local connector id", err)
}

func (d *DB) LocalConnectorByHash(tokenHash string) (LocalConnector, error) {
	var c LocalConnector
	err := d.QueryRow(`SELECT id, user_id, token_hash, label, created_at, last_seen_at
		FROM local_connectors WHERE token_hash = ?`, tokenHash).
		Scan(&c.ID, &c.UserID, &c.TokenHash, &c.Label, &c.CreatedAt, &c.LastSeenAt)
	return c, wrap("local connector by hash", err)
}

func (d *DB) LocalConnectorTouch(id int64) error {
	_, err := d.Exec(`UPDATE local_connectors SET last_seen_at = ? WHERE id = ?`, now(), id)
	return wrap("local connector touch", err)
}

func (d *DB) LocalConnectorsDeleteForUser(userID int64) error {
	_, err := d.Exec(`DELETE FROM local_connectors WHERE user_id = ?`, userID)
	return wrap("local connectors delete", err)
}

// ── settings (платформенные KV-настройки; значение шифрует вызывающий) ──────

func (d *DB) SettingSet(key, value string) error {
	_, err := d.Exec(`INSERT INTO settings(key, value, updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now())
	return wrap("setting set", err)
}

func (d *DB) SettingGet(key string) (string, error) {
	var value string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, wrap("setting get", err)
}

// UpdateWorkspaceProvider — смена Base URLs/моделей воркспейса (ротация).
func (d *DB) UpdateWorkspaceProvider(id, baseURL string, baseURLs []string, model string, models []string) error {
	if len(baseURLs) == 0 && baseURL != "" {
		baseURLs = []string{baseURL}
	}
	_, err := d.Exec(`UPDATE workspaces SET base_url = ?, base_urls = ?, model = ?, models = ?,
		updated_at = ? WHERE id = ?`, baseURL, EncodeBaseURLs(baseURLs), model, EncodeModels(models), now(), id)
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

// ── jira_runs: поллер метки agent-run (спайк jira-trigger) ──────────────────

// JiraRunInsert — зафиксировать новую задачу-запуск. Повтор ключа — no-op.
func (d *DB) JiraRunInsert(jiraKey, workspaceID string) error {
	_, err := d.Exec(`INSERT OR IGNORE INTO jira_runs(jira_key, workspace_id) VALUES (?, ?)`,
		jiraKey, workspaceID)
	return wrap("jira run insert", err)
}

// JiraRunUpdate — статус/сессия запуска (queued|injected|running|done|failed).
func (d *DB) JiraRunUpdate(jiraKey, status, session string) error {
	_, err := d.Exec(`UPDATE jira_runs SET status = ?, session = ?, updated_at = datetime('now') WHERE jira_key = ?`,
		status, session, jiraKey)
	return wrap("jira run update", err)
}

// JiraRunList — все запуски (журнал поллера, свежие сверху).
func (d *DB) JiraRunList(limit int) ([]map[string]string, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := d.Query(`SELECT jira_key, workspace_id, session, status, created_at, updated_at
		FROM jira_runs ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, wrap("jira run list", err)
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		m := map[string]string{}
		var key, ws, sess, st, ca, ua string
		if err := rows.Scan(&key, &ws, &sess, &st, &ca, &ua); err != nil {
			return nil, wrap("jira run scan", err)
		}
		m["jiraKey"], m["workspaceId"], m["session"], m["status"], m["createdAt"], m["updatedAt"] = key, ws, sess, st, ca, ua
		out = append(out, m)
	}
	return out, rows.Err()
}

// WorkspaceRequest — заявка на серверное рабочее место (требует апрув
// администратора). Payload — зашифрованный JSON провайдера; расшифровывает
// только платформа в момент approve.
type WorkspaceRequest struct {
	ID          string
	UserID      int64
	Name        string
	Kit         string
	Components  []string
	Payload     string // ciphertext
	SSHPubKey   string
	Status      string // pending | approved | rejected
	DecidedBy   string
	DecidedAt   string
	Reason      string
	WorkspaceID string
	CreatedAt   string
	UpdatedAt   string
	Login       string // login, заполняется при join
}

func scanWSReq(row interface{ Scan(...any) error }) (WorkspaceRequest, error) {
	var r WorkspaceRequest
	var componentsRaw string
	err := row.Scan(&r.ID, &r.UserID, &r.Name, &r.Kit, &componentsRaw, &r.Payload,
		&r.SSHPubKey, &r.Status, &r.DecidedBy, &r.DecidedAt, &r.Reason,
		&r.WorkspaceID, &r.CreatedAt, &r.UpdatedAt)
	if err == nil {
		r.Components = DecodeIDs(componentsRaw)
	}
	return r, err
}

func (d *DB) InsertWorkspaceRequest(r WorkspaceRequest) error {
	_, err := d.Exec(`INSERT INTO workspace_requests(id, user_id, name, kit, components,
		payload, ssh_pub_key, status, decided_by, decided_at, reason, workspace_id,
		created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.UserID, r.Name, r.Kit, EncodeIDs(r.Components), r.Payload,
		r.SSHPubKey, r.Status, r.DecidedBy, r.DecidedAt, r.Reason,
		r.WorkspaceID, r.CreatedAt, r.UpdatedAt)
	return wrap("insert workspace request", err)
}

const wsReqCols = `r.id, r.user_id, r.name, r.kit, r.components, r.payload,
	r.ssh_pub_key, r.status, r.decided_by, r.decided_at, r.reason,
	r.workspace_id, r.created_at, r.updated_at`

func (d *DB) WorkspaceRequestByID(id string) (WorkspaceRequest, error) {
	r, err := scanWSReq(d.QueryRow(`SELECT `+wsReqCols+` FROM workspace_requests r WHERE r.id = ?`, id))
	return r, wrap("workspace request by id", err)
}

func (d *DB) WorkspaceRequestsByUser(userID int64) ([]WorkspaceRequest, error) {
	rows, err := d.Query(`SELECT `+wsReqCols+`, u.login FROM workspace_requests r
		JOIN users u ON u.id = r.user_id WHERE r.user_id = ? ORDER BY r.created_at DESC`, userID)
	if err != nil {
		return nil, wrap("ws requests by user", err)
	}
	defer rows.Close()
	return d.wsReqList(rows)
}

func (d *DB) WorkspaceRequestsAll() ([]WorkspaceRequest, error) {
	rows, err := d.Query(`SELECT ` + wsReqCols + `, u.login FROM workspace_requests r
		JOIN users u ON u.id = r.user_id ORDER BY r.created_at DESC`)
	if err != nil {
		return nil, wrap("ws requests all", err)
	}
	defer rows.Close()
	return d.wsReqList(rows)
}

func (d *DB) wsReqList(rows *sql.Rows) ([]WorkspaceRequest, error) {
	var out []WorkspaceRequest
	for rows.Next() {
		var r WorkspaceRequest
		var componentsRaw, login string
		if err := rows.Scan(&r.ID, &r.UserID, &r.Name, &r.Kit, &componentsRaw, &r.Payload,
			&r.SSHPubKey, &r.Status, &r.DecidedBy, &r.DecidedAt, &r.Reason,
			&r.WorkspaceID, &r.CreatedAt, &r.UpdatedAt, &login); err != nil {
			return nil, wrap("ws requests scan", err)
		}
		r.Components = DecodeIDs(componentsRaw)
		r.Login = login
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) UpdateWorkspaceRequestStatus(id, status, decidedBy, reason, workspaceID string) error {
	_, err := d.Exec(`UPDATE workspace_requests SET status = ?, decided_by = ?,
		decided_at = ?, reason = ?, workspace_id = ?, updated_at = ? WHERE id = ?`,
		status, decidedBy, now(), reason, workspaceID, now(), id)
	return wrap("update ws request status", err)
}
