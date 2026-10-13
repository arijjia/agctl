---
schema: 1.0.0
id: secure-workspace-gateway
title: "Закрыть DSH Web за авторизованным same-origin gateway"
status: done
priority: P1
size: L
area: security
repo: arijjia/agctl
created: 2026-10-13
updated: 2026-10-13
tags: ["night"]
depends: []
epic: night-backlog-sweep-2026-10-12
---
## Описание

Связанный контракт: `arijjia/agctl-hq/backlog/open/2026-10-04-secure-dsh-gateway.md`.
Заменить небезопасный redirect на порт DSH на same-origin reverse proxy
`/w/{id}/`: проверять сессию и владельца на каждом HTTP/WS/SSE запросе,
пересылать только на loopback container port. Boot-токен добавлять только
server-to-server при upstream 401; использовать встроенный DSH cookie-mint,
переписывать Location и scope-ить cookie путём workspace. TLS заканчивается
на доверенном ingress, agctl backend остаётся внутренним.

## План

- Реализовать auth/owner-checked proxy, WS Upgrade и SSE flush.
- Добавить `--secure-cookies`; document ingress/TLS и остаточные риски Docker logs
  и loopback host.
- E2E: чужой 403, owner 200, 308 canonical slash, DSH mint-cookie и asset через
  same-origin; unit tests на SSE/WebSocket и отказ без boot-токена.

## Критерии готовности

- Доки содержат схему угроз и честные ограничения: ingress TLS внешний, boot URL
  остаётся в privileged docker logs, loopback доступен процессам самого хоста.
- Go tests закрывают owner/foreign/anonymous, cookie mint, scoped/Secure cookie,
  Location, path rewrite, SSE и WebSocket.
- Изолированный DSH 0.2.0 e2e подтверждает: URL без token, JS asset через gateway,
  только владелец открывает место, Docker port host bind = 127.0.0.1.

## Не делать

- Не выставлять raw Docker ports наружу; не заявлять, что agctl сам терминирует TLS
  или что это автоматически соответствует ФСТЭК/152-ФЗ.

## Журнал

- 2026-10-13: создано (`gitctl backlog new`).
- 2026-10-14: реализовано в commit `6ec8395`; docs `docs/secure-gateway.md`.
  `go vet ./...`, `go test ./...`, `go test -race ./...` PASS; e2e PASS 35/0,
  CHAT=PASS; WS/SSE и mint unit tests PASS; SPA Vite 8 build, npm audit 0.
  Remote ingress/TLS не развертывался; TLS termination и внешний сетевой probe
  остаются deployment-проверкой ЦИТ.
- 2026-10-13: done — Gateway реализован и проверен unit/race/e2e; ingress TLS остаётся deployment шагом ЦИТ.
