---
schema: 1.0.0
id: local-backend-websocket-bridge
title: "Локальный backend с server-hosted DSH frontend через outbound connector"
status: open
priority: P1
size: L
area: platform
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
- 2026-10-19: Панель сейчас показывает только честный локальный dsh web fallback. Цель: одна команда запускает локальный DSH runtime/connector, открывает outbound TLS/WebSocket к agctl, а frontend остаётся на серверном origin; проверить DSH browser/SDK protocol без форка, определить token grants/revoke, heartbeat и reconnect.
