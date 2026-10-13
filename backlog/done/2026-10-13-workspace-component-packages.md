---
schema: 1.0.0
id: workspace-component-packages
title: "Стартовые комплекты из типовых компонентов (манифест + галочки)"
status: done
priority: P2
size: L
area: plugin
repo: arijjia/agctl
created: 2026-10-13
updated: 2026-10-13
tags: ["night"]
depends: []
epic: night-backlog-sweep-2026-10-12
---
## Описание

Связанный контракт: `arijjia/agctl-hq/backlog/open/2026-10-12-workspace-component-packages.md`.
Сделать из двух жёстких профилей репки стартовые комплекты: versioned манифест
встроенных компонентов, выбор галочками в UI (all-on по умолчанию), compose
файлов в provision по порядку манифеста, сохранение состава в SQLite и
`agctl-kit.json` внутри рабочей репы.

## План

- `internal/provision/kits.go`: Kit/KitComponent, NormalizeComponents (порядок
  манифеста, отказ чужих/пустых ID), CopySelectedComponents, WriteKitManifest.
- SQLite `005_workspace_components.sql`, API `/api/kits`, поле `kit` + `components`
  при создании, UI чекбоксы.
- Legacy `/api/templates` и поле `template` оставить совместимыми.
- Каталог организации/публикация пользовательских компонентов — НЕ в этой
  карточке (контракт skills-tools-catalog в agctl-hq).

## Критерии готовности

- Два места из одного состава имеют идентичные файлы и `agctl-kit.json`.
- Снятая галочка реально исключает файлы компонента (unit + e2e).
- Дефолтный путь — «одна кнопка»: если клиент не прислал components,
  разворачивается полный default состав комплекта.
- Go test/vet/race PASS; изолированный e2e с выбором подмножества PASS.

## Не делать

- Не называть комплекты «шаблонами» в UI; не исполнять пользовательские
  компоненты без grants (зона каталога).

## Журнал

- 2026-10-13: создано (`gitctl backlog new`).
- 2026-10-14: реализовано (commits `f67af61`, `216d5de`); kit manifest v1.0.0
  (base: core-workspace/core-instructions; 1c-vibe: +1c-instructions/tooling/
  demo-project/skill), SQLite-состав, `/api/kits`, чекбоксы, `agctl-kit.json`.
  go test/vet PASS, e2e PASS 38/0 с подмножеством компонентов. Пользовательская
  публикация компонентов и версии манифеста для внешних kits — следующая итерация.
- 2026-10-13: done — Манифест+галочки+агctl-kit.json реализованы; публикация в каталог организации — отдельная итерация (skills-tools-catalog).
