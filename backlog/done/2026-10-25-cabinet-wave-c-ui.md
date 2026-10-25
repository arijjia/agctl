---
schema: 1.0.0
id: cabinet-wave-c-ui
title: "Кабинет v3: заявка роли developer, SSH-ключ, демонтаж формы мест"
status: done
priority: P2
size: M
area: plugin
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
epic: v3-unified-runtime
---
## Описание

Кабинет под v3: убрать UI создания dev-мест (демонтаж), добавить карточки
«Роль developer» (заявка + апрув-очередь для админа) и «SSH-канал»
(pubkey + живой sshPort). Реализуется волной C эпика v3-unified-runtime.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-10-25: Реализовано в агент/v3-waves-bcd: Кабинет v3 (заявка роли, SSH, проекты), legacy UI удалён; зеркала синхронны, node --test 5/5
- 2026-10-25: done — Кабинет v3 влит в ветку волн B-D
