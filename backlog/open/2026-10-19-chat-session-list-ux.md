---
schema: 1.0.0
id: chat-session-list-ux
title: "Режим Chat: показать сессии в одном дружелюбном списке вместо DSH Ungrouped"
status: open
priority: P2
size: S
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
- 2026-10-19: Текущий Host plugin назначает индивидуальный cwd и снимает workspaceId, поэтому DSH rc2 показывает сессии в Ungrouped и оставляет пустой default Workspace. Проработать UI grouping без отдельной видимой Workspace-папки на каждую беседу; не ломать per-session cwd.
