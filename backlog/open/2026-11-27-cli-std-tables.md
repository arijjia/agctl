---
schema: 1.0.0
id: cli-std-tables
title: "cli-std 1.1.0: list-команды agctl — выровненная таблица по умолчанию и глифы вердиктов, --json для машин"
status: open
priority: P3
size: M
area: cli
repo: arijjia/agctl
created: 2026-11-27
updated: 2026-11-27
tags: [cli-std]
depends: [cli-std-markers]
---
## Описание

`gitctl cli audit` 27.11: метки ✓, установка ✓, но `cli-std: 1.0.0 outdated`
(канон 1.1.0). Версия 1.1.0 требует табличного вывода list-команд по
умолчанию (формат и рецепт рендерера — §3.1 reference.md скилла cli-std,
референс `CLITable` в osx-git) со статус-глифами ✓/✗/📋/✅ в
колонках-вердиктах и `--json` для машин.

Кандидаты: `agctl dev secrets list`, `dev certs list`, `dev bridge list|pending|log`,
`dev images` (уже tabwriter — сверить формат), `dev status`, `backlog list|top`;
у agctl-server — `sync list` (вывод mutagen как есть — решить).

## Критерии готовности

- list-команды печатают таблицу по §3.1, `--json` там, где есть данные;
- `gitctl cli audit` для agctl и agctl-server — ✓ ok, `cli-std: 1.1.0` в repo.md.
