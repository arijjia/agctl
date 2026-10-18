---
schema: 1.0.0
id: cabinet-panel-e2e
title: "Авто-e2e панели кабинета: boot graph + agctl API-цикл без ручного клика"
status: done
priority: P1
size: S
area: image
repo: arijjia/agctl
created: 2026-10-18
updated: 2026-10-18
tags: ["e2e", "night-shift"]
depends: []
epic: night-dsh-plugins
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-18: создано (`gitctl backlog new`).
- 2026-10-18: done — scripts/e2e-cabinet.sh: agctl serve+register+create+running+шлюз-grep панели (docker-секция) + night-profile boot graph (curl-секция). Прогон 2026-10-18 23:57: PASS=4 FAIL=0. CI-ready (go build + docker).
- 2026-10-18: Дополнение (утро 2026-10-19): полноценный browser-уровень добавлен — tests/e2e-ui (Playwright): панель в сайдбаре, CORS-логин, создание workspace с LLM-провайдером и опциональным SSH-ключом через настоящий UI. Прогон: 3 PASS против живых :3555 + agctl:18933 (скриншот agctl-hq/logs/2026-10-19-night-playwright-cabinet.png). Попутный фикс панели: /api/me возвращает user-объект — парсинг исправлен (был 'Вошёл как undefined'); добавлены поля LLM Base URL/модель/токен и textarea SSH-ключа.
