---
schema: 1.0.0
id: cabinet-plugin-in-image
title: "Плагин кабинета в образ agctl-dsh — Tier-2 панель из коробки, same-origin за шлюзом"
status: done
priority: P1
size: M
area: image
repo: arijjia/agctl
created: 2026-10-18
updated: 2026-10-18
tags: ["dsh-client-plugin", "night-shift"]
depends: []
epic: night-dsh-plugins
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-18: создано (`gitctl backlog new`).
- 2026-10-18: done — Плагин @arijjia/agctl-cabinet в internal/assets/dsh-skel/profiles/web/node_modules (+bundles скелета) — SeedDshHome сеет панель в каждый воркспейс; same-origin за шлюзом (пустой URL → /api/* с cookie agctl). Анти-дрейф: internal/assets/assets_test.go (байт-в-байт с dsh-plugins/cabinet). Верификация: образ agctl-dsh:0.2.0-night1, воркспейс running, шлюз /w/<id>/ boot graph содержит панель.
