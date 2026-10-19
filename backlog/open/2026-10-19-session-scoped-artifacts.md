---
schema: 1.0.0
id: session-scoped-artifacts
title: "Файлы режима Chat: изоляция и persistence на уровне DSH session"
status: open
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
- 2026-10-19: Текущая реализация хранит DSH_HOME и work на пользователя; выделить sessionId→persistent directory и гарантировать file-tool scope для выбранной беседы; удаление/архивация по retention policy.
