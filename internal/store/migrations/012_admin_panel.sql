-- 012: админка супер-админа (карточка admin-panel-super-admin).
-- proxy_requests — журнал запросов пользователей, проксированных шлюзом
-- /chat/: метод, путь, статус, длительность. Тел запросов не храним.
-- quotas — бюджеты (токены) на роль или на отдельного пользователя: LiteLLM
-- ключ на платформу один, поэтому лимиты назначает agctl; инжект виртуальных
-- ключей LiteLLM — отдельная итерация (см. карточку).
CREATE TABLE proxy_requests (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	ts       TEXT NOT NULL DEFAULT (datetime('now')),
	user_id  INTEGER NOT NULL,
	login    TEXT NOT NULL DEFAULT '',
	method   TEXT NOT NULL,
	path     TEXT NOT NULL DEFAULT '',
	status   INTEGER NOT NULL DEFAULT 0,
	ms       INTEGER NOT NULL DEFAULT 0,
	bytes_out INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_proxyreq_user ON proxy_requests(user_id, id);
CREATE INDEX idx_proxyreq_ts ON proxy_requests(ts);

CREATE TABLE quotas (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	scope         TEXT NOT NULL CHECK (scope IN ('role', 'user')),
	subject       TEXT NOT NULL,
	daily_tokens  INTEGER NOT NULL DEFAULT 0,
	monthly_tokens INTEGER NOT NULL DEFAULT 0,
	note          TEXT NOT NULL DEFAULT '',
	updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
	UNIQUE (scope, subject)
);
