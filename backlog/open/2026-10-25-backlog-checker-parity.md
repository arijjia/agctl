---
schema: 1.1.0
id: backlog-checker-parity
title: "agctl backlog check: сровнять семантику id/файла с gitctl (legacy-карточки)"
status: open
priority: P3
size: S
area: meta
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-11-01
tags: []
depends: []
epic: two-mode-mvp
sessions:
  - session-46bfb32f-a37f-4058-aba7-caf4764cba76:work
---
## Описание

agctl backlog check требует id == имя-файла-без-даты и падает на двух
legacy-карточках, которые gitctl считает валидными. Сровнять семантику
(или перенести карточки), чтобы валидаторы не расходились.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-11-01: 2026-10-31: mismatch night-chat-polish устранён - файл переименован под id (2026-10-22-night-chat-polish-2026-10-22.md) при закрытии карточки. Остаётся второй mismatch: done/2026-10-12-night-backlog-sweep.md (id night-backlog-sweep-2026-10-12); agctl check при этом снисходителен - семантику валидаторов всё ещё надо сровнять.
