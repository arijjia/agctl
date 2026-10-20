---
schema: 1.0.0
id: chat-composer-copy
title: "Chat mode: локализовать composer hint после открытия сессии"
status: open
priority: P2
size: S
area: plugin
repo: arijjia/agctl
created: 2026-10-20
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

- 2026-10-20: создано (`gitctl backlog new`).
- 2026-10-20: После активной Session React снова выставляет composer data-placeholder “Message or run a task, / commands, @ files or sessions”; пользовательские add-files/access-mode/right-file/terminal controls скрыты. Прямой DOM setAttribute при boot перезаписывается. Нужен поддерживаемый DSH locale/slot override (или конфигурация), чтобы дать короткий русский текст; не monkey-patch глобальный DOM. Опционально проверить, нужен ли в Chat mode tab Trajectory.
