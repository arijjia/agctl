-- 002: выбор шаблона репки при создании воркспейса.
ALTER TABLE workspaces ADD COLUMN template TEXT NOT NULL DEFAULT 'base';
