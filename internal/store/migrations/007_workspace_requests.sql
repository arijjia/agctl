-- 007: заявки на рабочие места (режим «Разработка», опция require-approval).
-- payload — зашифрованный (AES-256-GCM) JSON провайдера; в открытом виде
-- хранятся только метаданные и SSH public key.
CREATE TABLE workspace_requests (
	id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	kit TEXT NOT NULL DEFAULT 'base',
	components TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL,
	ssh_pub_key TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT 'pending',
	decided_by TEXT NOT NULL DEFAULT '',
	decided_at TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	workspace_id TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX idx_wsreq_user ON workspace_requests(user_id, status);
CREATE INDEX idx_wsreq_status ON workspace_requests(status);
