---
schema: 1.0.0
id: egress-allowlist
title: "Egress-allowlist контейнеров: сеть только до LLM/хоста (opt-in флаг)"
status: open
priority: P2
size: L
area: infra
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

Контейнеры сегодня имеют неограниченный egress. Opt-in флаг agctl:
выделять сеть agctl и ставить DOCKER-USER правила — наружу только DNS и
host-gateway (LiteLLM). Решение владельца об умолчании — отдельно.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
