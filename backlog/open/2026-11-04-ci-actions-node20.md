---
schema: 1.1.0
id: ci-actions-node20
title: "Обновить GitHub Actions (checkout@v4, setup-go@v5, setup-node@v4, gitleaks@v2) — Node 20 deprecated"
status: in-progress
priority: P3
size: S
area: infra
repo: arijjia/agctl
created: 2026-11-04
updated: 2026-11-12
tags: ["ci", "deps"]
depends: []
sessions:
  - session-e0b7e7d5-7e13-4af9-879a-4cee65527862:work
  - session-365c2d55-6a2b-4739-99bd-914641f176f3:work
---
## Описание

В логах каждого CI-прогона предупреждение: экшены на Node 20
deprecated и форсируются на Node 24 — `actions/checkout@v4`,
`actions/setup-go@v5`, `actions/setup-node@v4`, `gitleaks/gitleaks-action@v2`
(также `peter-evans/create-pull-request@v7` в dsh-bump). Пока работает,
но форсированный запуск — риск поломки при изменении поведения раннера.

Поднять мажорные версии экшенов в `.github/workflows/ci.yml` и
`dsh-bump.yml` до актуальных (Node 24+), проверить прогон.

## Критерии готовности

- Все экшены в обоих воркфлоу на актуальных мажорах.
- Полный прогон ci без deprecation-предупреждений Node 20.

## Журнал

- 2026-11-04: создано (`gitctl backlog new`).
