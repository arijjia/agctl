---
schema: 1.1.0
id: backlog-checker-parity
title: "agctl backlog check: сровнять семантику id/файла с gitctl (legacy-карточки)"
status: done
priority: P3
size: S
area: meta
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-11-12
tags: []
depends: []
epic: two-mode-mvp
sessions:
  - session-46bfb32f-a37f-4058-aba7-caf4764cba76:work
  - session-f61b3c76-c5b1-4184-b046-86a58e758c75:work
---
## Описание

agctl backlog check требует id == имя-файла-без-даты и падает на двух
legacy-карточках, которые gitctl считает валидными. Сровнять семантику
(или перенести карточки), чтобы валидаторы не расходились.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-11-01: 2026-10-31: mismatch night-chat-polish устранён - файл переименован под id (2026-10-22-night-chat-polish-2026-10-22.md) при закрытии карточки. Остаётся второй mismatch: done/2026-10-12-night-backlog-sweep.md (id night-backlog-sweep-2026-10-12); agctl check при этом снисходителен - семантику валидаторов всё ещё надо сровнять.
- 2026-11-12: Сровнял семантику internal/backlog/card.go с gitctl: (1) schema валидна как semver с тем же MAJOR — 20 ложных FAIL на карточках, поднятых gitctl до 1.1.0 (sessions:), ушли; (2) легаси-паттерн «id с датой» (хвост id == created, файл без него) — валиден, как у gitctl; к моменту работы карточки data-mismatch уже был устранён (id night-backlog-sweep поправлен), поэтому менять/переносить карточки не потребовалось. Проверка: go test ./internal/backlog/ (новые TestValidateSchemaParity/TestValidateLegacyDatedID), agctl backlog check на репо = OK 64/0, gitctl backlog check = 0 invalid — валидаторы сходятся. Источник — код internal/backlog + прогон обоих чекеров; сессию-источник не читал, факты карточки подтверждены прогонами.
- 2026-11-12: refl: Дрейф валидаторов копился тихо: agctl-чекер не входил ни в один гард (CI/pre-commit гоняет gitctl). Кандидат: добавить agctl backlog check в pre-commit/CI, иначе parity снова разъедется.
- 2026-11-12: done — agctl backlog check сходится с gitctl на всех 64 карточках репо: schema по MAJOR, легаси датированные id валидны
