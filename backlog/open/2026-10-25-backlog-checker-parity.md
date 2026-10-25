---
schema: 1.0.0
id: backlog-checker-parity
title: "agctl backlog check: сровнять семантику id/файла с gitctl (legacy-карточки)"
status: open
priority: P3
size: S
area: meta
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

agctl backlog check требует id == имя-файла-без-даты и падает на двух
legacy-карточках, которые gitctl считает валидными. Сровнять семантику
(или перенести карточки), чтобы валидаторы не расходились.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
