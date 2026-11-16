---
schema: 1.0.0
id: drop-legacy-local-connector
title: "Удалить старый WSS-коннектор agctl local connect (internal/bridge)"
status: open
priority: P2
size: S
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-16
tags: ["legacy", "cleanup", "split-binaries"]
depends: []
---
## Описание

Старый экспериментальный коннектор `agctl local connect` (ноутбук сам
поднимает WSS-туннель к серверу, `--experimental-local-bridge`) заменён
локальным режимом `agctl dev` с мостом `internal/localbridge` и выключен по
умолчанию. Код остался: `internal/bridge`, `internal/server/local_bridge.go`,
`cmd/agctl/local.go`, маршруты `/api/local-backend/*`, методы
`store.LocalConnector*`, `bin/e2e-local-bridge.sh`. ≈1,1 тыс. строк и главный
источник путаницы имён «local» / «bridge» при чтении кода. Первый шаг к
разделению бинарей (`split-binaries-local-server`).

## Критерии готовности

- [ ] Удалены пакет, ручки, CLI-подкоманда `local`, флаг serve, e2e-скрипт,
      методы store, автодополнение и справка.
- [ ] Таблица `local_connectors` удаляется миграцией (008 не трогаем).
- [ ] `go vet`, `go test ./...` зелёные; документация без упоминаний коннектора
      кроме истории.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры (один бинарь на три режима).
