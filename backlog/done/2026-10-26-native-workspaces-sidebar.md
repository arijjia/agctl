---
schema: 1.0.0
id: native-workspaces-sidebar
title: "Вернуть нативный DSH Workspaces sidebar и project create-flow"
status: done
priority: P1
size: M
area: plugin
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
epic: night-chat-polish-2026-10-22
---
## Описание

Кастомное дерево/статусы в chat-brand расходятся с dsh: заменить их целиком
на нативный Workspaces browser, сохранив агctl-модель `/workspace/<project>`.
Нативная кнопка Add Workspace должна создавать пустой проект или (для
разработчика/admin) клонировать публичный GitHub HTTPS URL. Никаких hidden
per-session Workspace для новых чатов и пользовательских start/stop runtime.

## Критерии готовности

- Нативный DSH sidebar видим без переименования; штатные folder icons/open
  states, session statuses, search, filter/grouping и New Session доступны.
- При старте host-plugin регистрирует существующие `/workspace/*` каталоги как
  DSH Workspaces; dot/legacy `.sessions/` пропускает.
- Родная кнопка Add Workspace открывает flow «Пустой / GitHub»; обычный user не
  видит clone URL, developer/admin может клонировать только публичный GitHub.
- После POST `/api/projects` клиент вызывает `workspaces.create({path})`; новая
  native Workspace сразу видна, session в ней работает с проектным cwd.
- Новые default Sessions используют native default Workspace `/workspace`;
  старые `.sessions/` данные не мигрируются и не удаляются.
- Кастомное folder tree, зелёные квадраты, rename «Сессии», «Новая папка» и
  скрытие native controls удалены; зеркала plugin/test синхронны.

## План

- Заменить host chat-session-files per-session interceptor на boot registration
  project directories и удалить `AGCTL_CHAT_SESSION_ROOT`.
- Удалить кастомный sidebar из chat-brand, оставить branding/locale и
  расширение native Add Workspace.
- Переписать tests/docs, синхронизировать embedded profile.


## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
- 2026-10-26: Готово: host chat-session-files v0.3 регистрирует /workspace/* в workspaceRegistry при старте (dot-каталоги пропущены), sessionController больше не оборачивается; AGCTL_CHAT_SESSION_ROOT и SessionScope удалены из Docker/K8s/provision. chat-brand v0.5: кастомное дерево/статусы/переименование снесены, родная кнопка Add workspace открывает диалог «Пустой / GitHub» (clone только developer/admin), регистрация через workspaces.create({path}) из ответа API. Русский hint — locale contract.
- 2026-10-26: done — Нативный Workspaces sidebar + create-flow
