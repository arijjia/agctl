---
schema: 1.0.0
id: dsh-bump-next-channel
title: "dsh-bump: следить за тегом next, а не только latest"
status: open
priority: P2
size: S
area: ci
repo: arijjia/agctl
created: 2026-11-13
updated: 2026-11-13
tags: ["dsh", "ci", "deps"]
depends: []
---
## Описание

`bin/dsh-bump.sh` и воркфлоу `dsh-bump` сверяют пин dsh с npm dist-tag
`latest`. На 2026-11-13 `latest` = 0.2.2-rc.1, а свежие сборки выходят под
тегом `next` (0.2.2-rc.3): скрипт отставал на две версии и не видел
поломок контракта плагинов. При переходе на rc.3 три поломки
(agentPreset в startSession, штатная русская локаль dsh, русский Preview
Notice) нашлись только e2e-тестами и вручную. Обновления dsh разработчикам
локального режима пойдут через наш конвейер, поэтому ловить такие
изменения нужно раньше.

Сделать:
- `dsh-bump.sh --channel next|latest` (по умолчанию проверять оба и
  сообщать про `next`), `--update` до выбранного канала с той же
  rc-политикой.
- Воркфлоу: на появление новой версии в `next` — собрать образ на ней и
  прогнать `e2e-first-session` и тесты плагинов; результат — в issue или
  уведомление, без автомержа.

## Критерии готовности

- [ ] `bin/dsh-bump.sh` показывает и `latest`, и `next`; `--update
      --channel next` поднимает пин до `next`.
- [ ] Воркфлоу запускает e2e на версии из `next` и сообщает о поломке.
- [ ] Описано в README / docs (процесс обновления dsh).

## Журнал

- 2026-11-13: создано (`gitctl backlog new`).
