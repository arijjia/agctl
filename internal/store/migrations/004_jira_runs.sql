-- 004: связка задач Jira с рабочими местами (поллер метки agent-run).
-- jira_key уникален: одна задача = один запуск агента (повтор — руками).
CREATE TABLE jira_runs (
  jira_key     TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL,
  session      TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'queued',
  created_at   TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_jira_runs_ws ON jira_runs(workspace_id);
