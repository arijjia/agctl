-- Старый WSS-коннектор agctl local connect удалён (карточка
-- drop-legacy-local-connector); таблица pairing-записей больше не нужна.
DROP INDEX IF EXISTS idx_local_connectors_user;
DROP TABLE IF EXISTS local_connectors;
