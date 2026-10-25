---
schema: 1.0.0
id: dev-workspace-clone-url
title: "Dev-воркспейс: добавление клона git-репозитория по URL из Кабинета"
status: done
priority: P3
size: M
area: platform
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
---
## Описание
В настройках воркспейса для developer — поле «URL репозитория»: указанный git-репозиторий клонируется в новый каталог /workspace/<name> и открывается как воркспейс. Локальный invisible-autosave уже есть; подключение remote/push — отдельное решение с шифрованным хранением креденшелов и явным согласием пользователя.
(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-10-25: Сделано: POST /api/projects cloneUrl (http/https, developer/admin) — go-git клон в /workspace/<name>; без креденшелов; автопуш вынесен в git-remote-push-optin
- 2026-10-25: done — клон по URL реализован
