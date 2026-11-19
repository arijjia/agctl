-- Рабочие места лаунчера v2 поверх слотов (карточка developer-multi-containers):
-- рабочее место = слот пользователя (контейнер, свой том и dsh-home). Здесь —
-- то, чего у слота нет: имя, id для адреса /p/<id>/, папки хоста локального
-- режима и ревизия для правок из нескольких окон. Образ слота — в settings
-- (chat-image-u<ID>[-s<N>]), как у серверного лаунчера. Слот 0 (основное
-- место: папка и образ из dev.yaml) здесь не хранится.
-- Внешнего ключа на users нет: владелец локального режима — не учётная запись.
CREATE TABLE places (
    user_id    INTEGER NOT NULL,
    id         TEXT    NOT NULL,
    slot       INTEGER NOT NULL CHECK (slot > 0),
    name       TEXT    NOT NULL,
    mounts     TEXT    NOT NULL DEFAULT '[]',
    revision   INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL,
    PRIMARY KEY (user_id, id),
    UNIQUE (user_id, slot)
);
