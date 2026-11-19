---
schema: 1.0.0
id: legacy-workspace-code-cleanup
title: "Удалить dormant legacy workspace-код и /w compat-proxy после пилота"
status: done
priority: P3
size: L
area: platform
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-11-19
tags: ["v3", "cleanup"]
depends: []
---
## Описание

v3 в проде, e2e-смоуки зелёные, пилот прошёл — дормантный код рабочих
мест версии 2 удалён целиком: маршруты, шлюз, заявки, Jira-поллер,
store-модели и v2-провижининг. Миграции схемы прежних таблиц остаются
(append-only): `002`–`007` не откатываются, старые базы открываются.

## Что удалено

- Модуль `legacy-workspaces`: `/api/workspaces*` (включая 410-tombstone),
  `/api/templates`, `/api/kits`, заявки на рабочие места
  (`/api/workspace-requests*`), шлюз `/w/{id}/` и его страницы ожидания
  (`gateway.go`-хендлеры, `workspace_page.go`).
- Jira-поллер (`jirapoll.go`, модуль `jira`): спайк jira-trigger был
  привязан к `workspace_id` и не имел v3-потребителя очереди.
- Store: `Workspace`, `WorkspaceRequest`, `jira_runs`-модели и запросы;
  тест-хелперы `waitStatus`/`wsIDof`/`validCreate`.
- Provision v2: `Templates`, `Provision`, `ProvisionWithComponents`,
  `Paths`, `NewID`, `kits.go` (каталог комплектов), ассет
  `templates/repo` (20 КБ из бинаря); `Layout` оставлен — его используют
  чат-рантаймы.
- Флаг `RequireApproval` и его выдача в `/api/config`.
- Транспорт шлюза (`mintTransport`, cookie/path-хелперы) остался — им
  пользуется прокси `/chat/`; тесты `gateway_test.go` переписаны с `/w/`
  на `/chat/` (фикстура `plantChat` вместо `runningWorkspace`).

## Контракт

Старые клиенты получают 404/405 вместо 410 — надгробия сняты вместе с
маршрутами. `bin/e2e-smoke.sh` обновлён (POST /api/workspaces → 404/405).

## Критерии готовности

- [x] Маршруты v2 не отвечают (404/405), ни один не запускает рантайм.
- [x] `go vet ./... && go test ./...` зелёные; SPA собирается.
- [x] `bin/e2e-smoke.sh` (FAKE_LLM=1) — 39 PASS / 0 FAIL.
- [x] Миграции прежних таблиц сохранены; старые базы открываются без
      ошибок (`store.Open` гоняет миграции до актуальной схемы).

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-11-19: выполнено в ветке agent/legacy-workspace-cleanup после
  merge origin/main (раздельные бинари) — минус ~2000 строк.
