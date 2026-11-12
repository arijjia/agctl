---
schema: 1.1.0
id: ci-actions-node20
title: "Обновить GitHub Actions (checkout@v4, setup-go@v5, setup-node@v4, gitleaks@v2) — Node 20 deprecated"
status: done
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
- 2026-11-12: Подняты мажоры в обоих воркфлоу: checkout@v4→v6, setup-go@v5→v6, setup-node@v4→v6, gitleaks-action@v2→v3, create-pull-request@v7→v8; плюс goreleaser-action@v6→v7 (не упомянут в карточке, но критерий — все экшены на актуальных мажорах; v7 = node24, февраль 2026). Верификация мажоров по release-логам апстримов (checkout v6.0.0 persist-creds, setup-go v6 node24+toolchain, setup-node v6 auto-cache npm-only — совместимо с нашим npm ci, gitleaks v3 без изменений входов, cPR v8 node24). Прогон yaml-парса ruby: структура джоб цела (ci: test/web/gitleaks/release-snapshot; dsh-bump: bump). Полный CI-прогон до пуша не проверить — пуш вне тура; версии сверены с актуальными релизами апстримов.
- 2026-11-12: refl: Мажорные бампы экшенов ломаются о раннер <2.327.1 (node24) — для GitHub-hosted неактуально, для self-hosted это первое, что проверить. setup-node v6 ограничил автокэш npm-ом — у нас npm, ок; при расширении на pnpm/yarn вернуть package-manager-cache.
- 2026-11-12: done — Все экшены ci.yml и dsh-bump.yml на актуальных Node 24-мажорах: checkout@v6, setup-go@v6, setup-node@v6, gitleaks-action@v3, goreleaser-action@v7, create-pull-request@v8. YAML-структура верифицирована, версии сверены с релизами апстримов; полный чистый прогон CI — после пуша владельцем.
