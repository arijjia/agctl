---
schema: 1.0.0
id: chat-session-list-ux
title: "Режим Chat: показать сессии в одном дружелюбном списке вместо DSH Ungrouped"
status: done
priority: P2
size: S
area: plugin
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-20
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
- 2026-10-20: Закрыто в wave 2026-10-20: Host создаёт скрытый Workspace на per-session cwd, чтобы DSH composer оставался активным; chat-brand показывает в сайдбаре только flat «Сессии», скрывает Workspace rows/actions, правую sidebar/files/terminal controls; Playwright создаёт Session мышью и отправляет prompt до fake LLM.
- 2026-10-20: done — Flat Chat session UI и New Session/send regression прошли Playwright.
- 2026-10-20: Дополнительно проверено 2026-10-20: DSH composer требовал workspaceId и оставался contenteditable=false при ungrouped Session. Host теперь создаёт внутренний per-session Workspace по скрытому cwd, сохраняя composer активным; Playwright PASS: реальный mouse click New Session → session.create 200 → prompt 200 → fake LLM reply, while Workspace rows/right sidebar controls are hidden.
