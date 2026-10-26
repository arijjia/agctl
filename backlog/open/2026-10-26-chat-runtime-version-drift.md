---
schema: 1.0.0
id: chat-runtime-version-drift
title: "Пересоздавать личный контейнер при несовпадении версии agctl (label-версия)"
status: open
priority: P2
size: S
area: runtime
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
- 2026-10-26: 2026-10-26: после make up старый контейнер продолжал работать со старым dsh web — пользователю понадобился ручной docker rm. Предложение: на старте serve сверять версию agctl (label на контейнере) с текущей и бонсить дрейф; плюс проверить кэширование /profiles/.../client.js в dsh — обновление плагина требует hard-refresh браузера.
