-- 006: SSH-канал воркспейса (concepts §9.2): публичный ключ агента-человека
-- и опубликованный на хосте порт контейнерного sshd (127.0.0.1).
ALTER TABLE workspaces ADD COLUMN ssh_pub_key TEXT NOT NULL DEFAULT '';
ALTER TABLE workspaces ADD COLUMN ssh_port INTEGER NOT NULL DEFAULT 0;
