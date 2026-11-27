---
schema: 1.0.0
id: cli-std-markers
title: "cli-std: метки инвазивности 🔍/✏️/⚠️ в комплишенах и справке agctl, agctl-server"
status: done
priority: P3
size: M
area: cli
repo: arijjia/agctl
created: 2026-11-27
updated: 2026-11-27
tags: [cli-std]
depends: []
---
Установка уже по cli-std (`make install link`: относительные симлинки в
~/.local/bin и ~/.config/fish/completions, комплишены в completions/ репо с
тестом на дрейф). Осталось ядро стандарта: каждая листовая подкоманда несёт
метку в описании — и в fish-комплишенах, и в `--help`/usage: 🔍 читающая
(status, paths, images, secrets list, certs list|check, llm-check?),
✏️ мутирующая (init, secrets set|rm, certs host|add|rm|fetch, update,
rollback), ⚠️ с побочным эффектом (up, down, open, llm-check — ходит в сеть).
Группы с единогласными листьями поднимают метку; тест обхода дерева команд
против дрейфа (прецедент gitctl CliStdMarkersTests). Затем `cli-std:` в
шапке repo.md.

## Журнал

- 2026-11-27: сделано — метки во всех справках (agctl, dev, dev certs,
  backlog; agctl-server, sync) и комплишенах; канон меток и тест
  TestCliStdMarks в обоих бинарях (проверены подменой метки); llm-check
  больше не переносит доступы из окружения — только читает и ходит в сеть.
  repo.md: cli-std 1.0.0.
