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
