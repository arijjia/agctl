---
schema: 1.0.0
id: chat-runtime-per-user
title: "Чат DSH: отдельный ленивый runtime и том на пользователя"
status: done
priority: P1
size: M
area: runtime
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-19
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: Критерии: после регистрации корень входит в /chat; каждый userID получает другой container и DSH_HOME/work; TTL усыпляет container без удаления тома; активный stream не прерывается; login/logout сохраняет user isolation; e2e на двух пользователях.
- 2026-10-19: done — Implemented per-user lazy dsh runtime, persistent DSH_HOME/work, 15m TTL reaper, active-stream guard and graceful shutdown; race suite and two-user Docker e2e passed.
