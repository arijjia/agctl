CREATE TABLE local_connectors (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    label       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);
CREATE INDEX idx_local_connectors_user ON local_connectors(user_id);
