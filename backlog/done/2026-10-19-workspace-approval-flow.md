---
schema: 1.0.0
id: workspace-approval-flow
title: "Кабинет: заявки на серверный workspace и approve/reject администратора"
status: done
priority: P1
size: M
area: plugin
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
- 2026-10-19: Критерии: require-approval flag; payload провайдера шифруется; user видит статус; admin видит pending requests и может approve/reject; quota и owner checks; Playwright покрывает заявка→approve→workspace.
- 2026-10-19: done — Implemented encrypted request payload, RequireApproval flag, owner/admin API, Cabinet request/approve/reject UI; TestApprovalFlow and Playwright root→chat→request→approve passed.
