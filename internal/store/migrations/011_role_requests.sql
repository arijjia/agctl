-- 011: роль developer (v3 unified runtime) — заявки с привязкой к учётке.
-- Заявку подаёт пользователь; после апрува админом users.role = 'developer'
-- (роль постоянная, привязана к учётной записи).
CREATE TABLE role_requests (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id INTEGER NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending',
	motivation TEXT NOT NULL DEFAULT '',
	decided_by TEXT NOT NULL DEFAULT '',
	decided_at TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_rolereq_user ON role_requests(user_id, status);
CREATE INDEX idx_rolereq_status ON role_requests(status);
