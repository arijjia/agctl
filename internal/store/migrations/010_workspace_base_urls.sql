-- 010: multiple provider endpoints per workspace (legacy base_url remains primary alias).
ALTER TABLE workspaces ADD COLUMN base_urls TEXT NOT NULL DEFAULT '';
