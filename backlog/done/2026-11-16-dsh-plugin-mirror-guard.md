---
schema: 1.1.0
id: dsh-plugin-mirror-guard
title: "Гард дрейфа зеркал плагинов: dsh-plugins ↔ internal/assets/dsh-skel (diff -r в CI/pre-commit)"
status: done
priority: P2
size: S
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-20
tags: ["ci", "plugins"]
depends: []
sessions:
  - session-cc649d5c-51d5-4a5b-b2d0-fabbb52f8752:work
---

- Описание: клиентские плагины живут двумя зеркалами — исходники в `dsh-plugins/<name>/` и встроенная копия в `internal/assets/dsh-skel/profiles/web/node_modules/@arijjia/agctl-<name>/` (её раздаёт EnsureChatPlugins). Рассинхрон ловится только глазами: фикс в одном зеркале не даёт эффекта в работающем чате, если не перенести во второе (на карточке v3-product-e2e-playwright правку пришлось зеркалить вручную). Нужен дешёвый гард: pre-commit хук или CI-шаг с `diff -r` по всем четырём бандлам (cabinet, chat-brand, chat-session-files, workspace-console), красный при любом расхождении.

- Критерии готовности: гард запускается в CI (и/или pre-commit) и падает при намеренном рассинхроне любого из четырёх зеркал; на текущем дереве зелёный; способ синхронизации описан в ответе гарда одной строкой (cp каталога).

## Журнал

- 2026-11-16: создано по итогам карточки v3-product-e2e-playwright (фикс перехвата RU-кнопки вручную зеркалился в dsh-skel; рекомендация из конца тура).
- 2026-11-16: создано (`gitctl backlog new`).
- 2026-11-20: Гард — Go-тест `internal/assets/mirror_test.go` (TestDshPluginMirrorsInSync): пары dsh-plugins/<n> ↔ @arijjia/agctl-<n> находит сам, сравнивает деревья байт в байт (node_modules и .DS_Store не в счёт), падает и на плагин без пары; в CI идёт шагом `go test ./...`. Проверено: на текущем дереве зелёный; намеренный рассинхрон (лишний файл в cabinet, правка package.json в chat-brand) — красный со списком файлов и строкой cp для синхронизации.
