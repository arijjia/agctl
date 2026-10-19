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
- 2026-10-19: Research 2026-10-19: dsh web browser Host checks require Origin=Host and refuse cross-site; direct server page→localhost DSH cannot work, and dsh --host 0.0.0.0 is unsupported. SDK stdio protocol has only initialize/prompt/wait/cwd + events, so it cannot by itself preserve full DSH Web UI. No-fork route: local connector opens outbound TLS/WebSocket tunnel; agctl authenticates it and proxies complete HTTP/SSE/WebSocket traffic to local dsh web with Host/Origin rewritten to loopback. Need multiplexing, one-time scoped token/revoke/reconnect and e2e.
