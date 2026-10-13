-- 005: persist selected built-in components for reproducible workspace creation.
ALTER TABLE workspaces ADD COLUMN components TEXT NOT NULL DEFAULT '';
