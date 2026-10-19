---
schema: 1.0.0
id: session-scoped-artifacts
title: "Файлы режима Chat: изоляция и persistence на уровне DSH session"
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
- 2026-10-19: Текущая реализация хранит DSH_HOME и work на пользователя; выделить sessionId→persistent directory и гарантировать file-tool scope для выбранной беседы; удаление/архивация по retention policy.
- 2026-10-19: Исследование DSH host API: Session Controller create принимает cwd; WorkingDirectoryService хранит workingDirectory projection per Session; dsh-tool-fs использует ctx.workingDirectory.ensure для файловых инструментов. Возможный plugin-хук system-prompt/assemble может выставлять /workspace/.sessions/<sessionId> без изменения DSH ядра и без смены Workspace header; нужно проверить порядок hooks и отсутствие лишнего model turn на живом DSH.
- 2026-10-19: Wave 1 complete: Host plugin intercepts sessionController.create in DSH 0.2.0-rc.2, stores each new Session at /workspace/.sessions/<sessionId>, session header cwd persists with per-user work volume; e2e via /chat/api/session/create + session/list confirms the actual cwd. Not a security boundary between same-user sessions.
- 2026-10-19: done — Per-session cwd and persistent path verified via Docker e2e.
- 2026-10-19: Verified against the real agctl Docker chat runtime on DSH 0.2.0-rc.2: session.create via same-origin /chat/api assigns /workspace/.sessions/<sessionId>; session/list returns the same cwd; e2e-cabinet PASS=12/12. Directory persists in per-user work volume; no security isolation between same-user sessions.
