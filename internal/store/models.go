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

// normalizeBaseURLs обрезает пробелы, выбрасывает пустые и дубли,
// сохраняя первый порядок следования.
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

// DecodeIDs читает JSON-список component IDs; пустая строка или битый
// JSON дают nil (компоненты опциональны).
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

// wrap приводит ошибки SQL к ошибкам домена: sql.ErrNoRows превращается в
// ErrNotFound, остальное оборачивается с именем операции.
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

// CreateUser добавляет учётную запись и возвращает её ID; занятый логин
// распознаётся по UNIQUE-ошибке и оборачивается человекочитаемой ошибкой.
func (d *DB) CreateUser(login, passwordHash, role string) (int64, error) {
	id, err := d.insertID(`INSERT INTO users(login, password_hash, role) VALUES(?,?,?)`,
		login, passwordHash, role)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "duplicate key") {
			return 0, fmt.Errorf("store: login занят: %w", err)
		}
		return 0, wrap("create user", err)
	}
	return id, nil
}

// CountUsers возвращает число учётных записей (нулевое = первый станет admin).
func (d *DB) CountUsers() (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, wrap("count users", err)
}

// scanUser сканирует строку users в структуру User (общая для всех выборок).
func scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Login, &u.PasswordHash, &u.Role, &u.CreatedAt)
	return u, err
}

// UserByLogin выбирает учётку по логину; нет записи → ErrNotFound.
func (d *DB) UserByLogin(login string) (User, error) {
	u, err := scanUser(d.QueryRow(
		`SELECT id, login, password_hash, role, created_at FROM users WHERE login = ?`, login))
	return u, wrap("user by login", err)
}

// UserByID выбирает учётку по идентификатору; нет записи → ErrNotFound.
func (d *DB) UserByID(id int64) (User, error) {
	u, err := scanUser(d.QueryRow(
		`SELECT id, login, password_hash, role, created_at FROM users WHERE id = ?`, id))
	return u, wrap("user by id", err)
}

// UsersList возвращает все учётные записи по возрастанию ID (админка).
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

// CreateSession сохраняет токен сессии с UTC-меткой истечения.
func (d *DB) CreateSession(token string, userID int64, expires time.Time) error {
	_, err := d.Exec(`INSERT INTO sessions(token, user_id, expires_at) VALUES(?,?,?)`,
		token, userID, expires.UTC().Format("2006-01-02 15:04:05"))
	return wrap("create session", err)
}

// SessionUser возвращает пользователя по живому (не истёкшему) токену;
// просроченный или неизвестный токен → ErrNotFound.
func (d *DB) SessionUser(token string) (User, error) {
	u, err := scanUser(d.QueryRow(`
		SELECT u.id, u.login, u.password_hash, u.role, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ? AND s.expires_at > datetime('now')`, token))
	return u, wrap("session user", err)
}

// DeleteSession удаляет сессию (logout).
func (d *DB) DeleteSession(token string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return wrap("delete session", err)
}

// DeleteExpiredSessions чистит истёкшие сессии (фоновая гигиена).
func (d *DB) DeleteExpiredSessions() error {
	_, err := d.Exec(`DELETE FROM sessions WHERE expires_at <= datetime('now')`)
	return wrap("delete expired sessions", err)
}

// ── workspaces ───────────────────────────────────────────────────────────────

const wsCols = `id, owner_id, name, template, status, repo_path, work_path, dsh_home,
	container_id, host_port, base_url, base_urls, model, models, components, error, created_at, updated_at,
	ssh_pub_key, ssh_port`

// scanWS сканирует строку workspaces, разворачивая TEXT-колонки
// (base_urls JSON, models, components) в поля структуры.
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

// InsertWorkspace добавляет workspace; занятое имя распознаётся по
// UNIQUE-ошибке. BaseURL без списка дублируется в base_urls.
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

// WorkspaceByID выбирает workspace по идентификатору; нет записи → ErrNotFound.
func (d *DB) WorkspaceByID(id string) (Workspace, error) {
	w, err := scanWS(d.QueryRow(`SELECT `+wsCols+` FROM workspaces WHERE id = ?`, id))
	return w, wrap("workspace by id", err)
}

// WorkspacesByOwner — все места пользователя, свежие сверху (legacy-поток).
func (d *DB) WorkspacesByOwner(ownerID int64) ([]Workspace, error) {
	return d.wsList(`SELECT `+wsCols+` FROM workspaces WHERE owner_id = ? ORDER BY created_at DESC`, ownerID)
}

// WorkspacesAll — все места платформы с логином владельца (админка).
func (d *DB) WorkspacesAll() ([]Workspace, error) {
	return d.wsList(`SELECT w.id, w.owner_id, w.name, w.template, w.status, w.repo_path, w.work_path,
		w.dsh_home, w.container_id, w.host_port, w.base_url, w.base_urls, w.model, w.models, w.components, w.error,
		w.created_at, w.updated_at, w.ssh_pub_key, w.ssh_port
		FROM workspaces w JOIN users u ON u.id = w.owner_id
		ORDER BY w.created_at DESC`)
}

// wsList исполняет готовый запрос списка workspaces и сканирует строки.
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

// UpdateWorkspaceStatus меняет статус и текст ошибки, трогая updated_at.
func (d *DB) UpdateWorkspaceStatus(id, status, errText string) error {
	_, err := d.Exec(`UPDATE workspaces SET status = ?, error = ?, updated_at = ? WHERE id = ?`,
		status, errText, now(), id)
	return wrap("update ws status", err)
}

// UpdateWorkspaceRuntime фиксирует привязку контейнера и опубликованный
// порт, сбрасывая текст ошибки и выставляя статус.
func (d *DB) UpdateWorkspaceRuntime(id, containerID string, hostPort int, status string) error {
	_, err := d.Exec(`UPDATE workspaces SET container_id = ?, host_port = ?, status = ?,
		error = '', updated_at = ? WHERE id = ?`, containerID, hostPort, status, now(), id)
	return wrap("update ws runtime", err)
}

// UpdateWorkspacePaths сохраняет хост-пути (repo/work/dsh-home) после
// создания места.
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

// DeleteWorkspace удаляет запись места (файлы чистит вызывающий).
func (d *DB) DeleteWorkspace(id string) error {
	_, err := d.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
	return wrap("delete workspace", err)
}

// ── secrets (LLM-токены, шифротекст) ────────────────────────────────────────

// SecretUpsert сохраняет настройки LLM-провайдера пользователя (upsert по
// user_id); ciphertext шифрует вызывающий (secrets.Manager).
func (d *DB) SecretUpsert(userID int64, baseURL, model string, models []string, ciphertext string) error {
	_, err := d.Exec(`INSERT INTO secrets(user_id, base_url, model, models, ciphertext, updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET base_url = excluded.base_url,
		  model = excluded.model, models = excluded.models,
		  ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`,
		userID, baseURL, model, EncodeModels(models), ciphertext, now())
	return wrap("secret upsert", err)
}

// SecretByUser возвращает сохранённые настройки провайдера пользователя;
// нет записи → ErrNotFound.
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

// ── settings (платформенные KV-настройки; значение шифрует вызывающий) ──────

// SettingSet пишет KV-настройку платформы (upsert по key); значение шифрует
// вызывающий.
func (d *DB) SettingSet(key, value string) error {
	_, err := d.Exec(`INSERT INTO settings(key, value, updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now())
	return wrap("setting set", err)
}

// SettingGet читает KV-настройку; нет ключа → ErrNotFound.
func (d *DB) SettingGet(key string) (string, error) {
	var value string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	return value, wrap("setting get", err)
}

// ── audit_log (РСБ) ─────────────────────────────────────────────────────────

// Audit дописывает событие в журнал безопасности (actor/action/target/details).
func (d *DB) Audit(actor, action, target, details string) error {
	_, err := d.Exec(`INSERT INTO audit_log(actor, action, target, details) VALUES(?,?,?,?)`,
		actor, action, target, details)
	return wrap("audit", err)
}

// AuditList возвращает свежие события журнала (лимит нормализуется, дефолт 200).
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
