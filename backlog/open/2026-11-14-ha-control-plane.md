---
schema: 1.5.0
id: ha-control-plane
title: "HA control-plane шлюза: PostgreSQL + leases"
status: open
priority: P3
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-14
updated: 2026-11-14
tags: ["kubernetes", "ha", "storage"]
depends: []
sessions:
  - session-c780be29-3c20-4064-a46e-9f9eb1cb8b92:work
---

## Описание

Шлюз agctl — single point of failure: состояние в одном SQLite
($AGCTL_HOME/agctl.db, modernc.org/sqlite), одна реплика пода шлюза в
deploy/k8s. Падение пода/узла = платформа недоступна, персональные
chat-рантаймы живут, но вход/cookies/TTL-рипер/агенты кабинета — нет.
Для инсталляций с SLA нужна горизонтальная масштабируемость control-plane.

Вводные:
- Хранилище — internal/store (SQLite, WAL); secrets зашифрованы мастер-ключом
  ($AGCTL_HOME/keys) — ключ тоже локальный файл, это первый SPOF после БД.
- Сессионные таймеры: TTL-рипер чатов идёт в цикле одного сервера
  (chat.go reapIdleChats), Sweeps дрейфа — тоже; при нескольких репликах
  нужен leader-election (k8s leases или pg advisory locks).
- chatRuntime in-memory map (chats, tokens) — при рестарте восстанавливается
  лениво из БД; для 2+ реплик достаточно stickiness не требуется, если
  ensureChat идемпотентен (он идемпотентен по RuntimeID).

## План

1. Решение владельца по стеку: PostgreSQL (driver + миграции из схемы store)
   vs RWO-PVC + failover; критерий — уже есть эксплуатируемый PG у профиля
   РСХБ или нет.
2. Абстракция store: вынести SQL-диалектные места, миграция схемы,
   crosstest текущих тестов store на обоих бэкендах.
3. Leader-под для фоновых циклов (TTL-рипер, prewarm, свип дрейфа):
   k8s coordination.k8s.io Lease с renewDeadline; фолбэк single-реплики —
   циклы в процессе, как сейчас.
4. deploy/k8s: replicas + PDB, readiness по БД-коннекту; docs.

## Критерии готовности

- kind: kill пода шлюза при 2 репликах — сессии пользователей переживают
  (cookie валиден, чат-рантаймы не дублируются), фоновые циклы не
  задваиваются (проверка логов обеих реплик).
- SQLite-режим по умолчанию не деградирует (single-реплика без leases).

## Не делать

- Не выносить мастер-ключ в k8s Secret в этом туре (отдельное решение по
  KMS/SOPS); не трогать chat-рантаймы (они уже per-user и независимы).

## Журнал

- 2026-11-13: создано из п.6 карточки k8s-substrate-ax-backend (draft, тело из роадмапа docs/architecture.md §6 + структуры internal/store/server).
- 2026-11-14: создано (`gitctl backlog new` --draft)
- 2026-11-14: gate: ready (попытка 1)
- 2026-11-14: gate passed, feedback cleared
- 2026-11-14: unpark: тело из роадмапа и кода store/server, gate ready с первой попытки
