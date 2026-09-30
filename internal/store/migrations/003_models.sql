-- 003: список моделей провайдера (первая = по умолчанию, model — legacy).
ALTER TABLE workspaces ADD COLUMN models TEXT NOT NULL DEFAULT '';
ALTER TABLE secrets ADD COLUMN models TEXT NOT NULL DEFAULT '';
