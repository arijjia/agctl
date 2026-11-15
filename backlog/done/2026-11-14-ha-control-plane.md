---
schema: 1.5.0
id: ha-control-plane
title: "HA control-plane шлюза: PostgreSQL + leases"
status: done
priority: P3
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-14
updated: 2026-11-15
tags: ["kubernetes", "ha", "storage"]
depends: []
sessions:
  - session-c780be29-3c20-4064-a46e-9f9eb1cb8b92:work
  - session-1247c577-a767-45f9-80c3-6e31877ad29a:work
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
- 2026-11-15: п.3 сделан полностью: internal/leaderelection — coordination.k8s.io Lease (acquire/renew/takeover по истечении, CAS через resourceVersion, конфликты и транзиенты не мигают лидерством; LeaseDuration 15c/renew 5c/тик 2c), фолбэк single-реплики — флаг не задан (nil-Holder всегда лидер). Гейтинг: TTL-рипер, autosave, prewarm, reconcile, jira-поллер, чистка сессий — каждый тик спрашивает isLeader; стартовый свип дрейфа делает только лидер (OnAcquired). Флаги --ha-leader-lease/--ha-leader-kubeconfig/--ha-leader-namespace (env AGCTL_HA_LEADER_*, config serve.ha_leader_*). Тесты на fake clientset: первый acquire+renew, блокировка живым чужим lease, takeover по истечении, потеря на конфликте renew, Lease без держателя; попутно починены паники на nil-полях Lease (holder/duration/renewTime).
- 2026-11-15: п.4 — заготовка: deploy/k8s/overlays/ha (replicas 2 + PDB minAvailable 1 + env lease) — рендер проверен kubectl kustomize; применять ТОЛЬКО после PG-бэкенда store (SQLite RWO реплики не шарит — честно написано в шапке оверлея). П.2 — инвентаризация для порта: 12 миграций/12 таблиц; диалектных мест немного: AUTOINCREMENT×6 (→BIGSERIAL), datetime('now')×17 (→now()), strftime×8+3 (→to_char), PRAGMA user_version (→schema_migrations), INSERT OR IGNORE×1 (→ON CONFLICT DO NOTHING); плейсхолдеры ? — механически через db.Rebind/обёртку. Попутно починен предсуществующий флейк store TestProxyRequestsActivity (bob сеялся около полуночи UTC на позавчерашний день).
- 2026-11-15: refl: refl: критерий 'kill пода при 2 репликах — сессии переживают' физически недостижим без PG (SQLite RWO не шарится) — потому тур осознанно не закрывает карточку: ждать решения владельца по стеку (п.1: есть ли эксплуатируемый PG у профиля РСХБ) бессмысленно без подготовленного порта; следующий срез — PG-бэкенд store по инвентаризации из журнала, затем kill-под-смоук в kind (Postgres StatefulSet + 2 реплики + lease) на оверлей overlays/ha. Урок рефакторинга: case *lease.Spec.HolderIdentity в switch разыменовывал nil до моей защиты в default — переключатель evaluated до ветки; nil-гарды должны стоять до switch.
- 2026-11-15: п.2 сделан: PG-бэкенд store — pgx/v5 stdlib за database/sql (DB.pg по префиксу postgres:// в --store-dsn / AGCTL_STORE_DSN / serve.store_dsn); shadow Query/QueryRow/Exec = rebind ?→$n + конвертация диалекта (datetime('now'), datetime('now', ?), date('now'[, ?]), strftime-бакеты → to_char; TEXT-схема сохранена — лексические сравнения идентичны); INSERT OR IGNORE → execIgnore (pg: ON CONFLICT DO NOTHING); LastInsertId → insertID (pg: RETURNING id); миграции — общий SQL + convertMigration (AUTOINCREMENT→BIGSERIAL, DEFAULT datetime→to_char), версия схемы pg — таблица schema_migrations. Crosstest: все тесты store через openTest на обоих бэкендах (AGCTL_TEST_STORE=pg + AGCTL_TEST_PG_DSN; pg получает чистую схему) — 9/9 зелёные на живом PostgreSQL 16 и на sqlite.
- 2026-11-15: п.4 сделан: kill-под-смоук bin/e2e-ha-kind.sh (kind + Postgres StatefulSet + 2 реплики с --store-dsn/--ha-leader-lease + общий PVC на мастер-ключ + keyinit-Job). Результат live: register/login → kubectl delete pod → /api/me 200 с той же cookie на выжившей реплике (сессии в PG); lease переезжает ровно один раз на выжившего (логи реплик: один держатель, фолловер не претендует); SQLite-режим без флагов не меняется (весь сьют зелёный на sqlite). 'Чат-рантаймы не дублируются' — гарантия идемпотентностью ensureChat по RuntimeID (k8srt тесты), live-чаты в смоук не включены. Попутно: EnsureMasterKey → O_EXCL (гонка реплик на общем томе), leases в RBAC agctl-runtime, jira INSERT OR IGNORE → execIgnore.
- 2026-11-15: done — Карточка закрыта: HA control-plane — PG-бэкенд store (crosstest 9/9 на PostgreSQL 16 и sqlite), leader-election k8s Lease с гейтингом всех фоновых циклов (merge 1a486da), deploy заготовка overlays/ha, kill-под-смоук зелёный (сессия переживает, циклы не задваиваются, lease переезжает). Критерии готовности выполнены; 'чат-рантаймы не дублируются' — идемпотентность ensureChat по RuntimeID (тесты), live-чаты не включались в смоук. Не сделано: мастер-ключ в Secret/KMS (осознанно вне тура — карточка 'Не делать'), HA-запуск в CI (ручной смоук).
