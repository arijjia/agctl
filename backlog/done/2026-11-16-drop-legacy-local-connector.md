---
schema: 1.0.0
id: drop-legacy-local-connector
title: "Удалить старый WSS-коннектор agctl local connect (internal/bridge)"
status: done
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

- [x] Удалены пакет, ручки, CLI-подкоманда `local`, флаг serve, e2e-скрипт,
      методы store, автодополнение и справка.
- [x] Таблица `local_connectors` удаляется миграцией (008 не трогаем).
- [x] `go vet`, `go test ./...` зелёные; документация без упоминаний коннектора
      кроме истории.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры (один бинарь на три режима).
- 2026-11-16: Сделано: удалены internal/bridge, server/local_bridge.go, cmd/agctl/local.go, маршруты /api/local-backend/*, флаг --experimental-local-bridge, store.LocalConnector*, bin/e2e-local-bridge.sh, справка/автодополнение; миграция 013 удаляет таблицу; go mod tidy убрал websocket-зависимость. go vet/test зелёные.
