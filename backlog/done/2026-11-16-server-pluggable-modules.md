---
schema: 1.0.0
id: server-pluggable-modules
title: "Серверные модули (админка, роли, квоты, prewarm, лаунчер, legacy workspaces) подключаются точкой входа, а не общим Routes()"
status: done
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-16
tags: ["architecture", "split-binaries"]
depends: ["drop-legacy-local-connector"]
---
## Описание

Сейчас все маршруты регистрирует общий `Server.Routes()`: админка, роли и
заявки, лаунчер developer'а, квоты LiteLLM, prewarm-пул, регистрация,
legacy workspaces `/w/{id}`. Поэтому серверный код линкуется и в локальный
режим, а локальное поведение держится на ветках `isLocal()`.

Нужно: ядро `internal/server` знает только общую часть (вход, `/api/me`,
проекты, каталог, MCP, `/chat/`, карта), а серверные части — модули
(`server.Module`: маршруты + фоновые задачи), которые подключает точка
входа через `Config.Modules`. Локальный режим модули не подключает: их кода
нет в графе вызовов локального бинаря, ветки `isLocal()` для них не нужны.

## Критерии готовности

- [x] `server.Module` и `server.ServerModules()`; `Routes()` и
      `StartBackground()` вызывают модули из `Config.Modules`.
- [x] В модули вынесены: регистрация, роли/SSH developer'а, лаунчер
      developer'а, админка, квоты LiteLLM, prewarm, legacy workspaces и `/w`,
      Jira-поллер.
- [x] Локальный режим работает без модулей (`agctl dev`); серверный — как
      раньше. Тесты сервера проходят, локальные тесты проверяют отсутствие
      серверных маршрутов.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры.
- 2026-11-16: Сделано: internal/server/modules.go — Module{Name, Routes, Background}, ServerModules(): registration, roles, launcher, admin, prewarm, autosave, legacy-workspaces (+/w), jira, ax-provider-config. Routes()/StartBackground() ядра регистрируют только общее; serve подключает ServerModules(), dev — нет. Ушли ветки isLocal() в handleRegister и автосейве. Квоты LiteLLM остались в ensureChat (выключены флагом). Тест TestLocalModeHasNoServerModules. go vet/test зелёные.
