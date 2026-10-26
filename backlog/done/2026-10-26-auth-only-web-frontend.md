---
schema: 1.0.0
id: auth-only-web-frontend
title: "Фронт agctl — только авторизация; /app/ и v2-вид удалены"
status: done
priority: P1
size: M
area: platform
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
- 2026-10-26: Готово: web/src урезан до Login (api.ts — только register/login/logout/me); SPA отдаётся с корня (no-store) без пути /app/, маршруты /app* удалены; после входа корень → /chat/. make up теперь зависит от img_build — после docker system prune образ agctl-dsh пересобирается сам (corp — тоже). tier0-fallback-status-page отменена как перекрытая этим решением.
- 2026-10-26: done — Auth-only фронт + self-healing make up
