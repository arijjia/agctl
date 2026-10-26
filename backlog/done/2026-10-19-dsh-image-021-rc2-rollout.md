---
schema: 1.0.0
id: dsh-image-021-rc2-rollout
title: "Накатить DSH 0.2.1-rc.2 и пересобрать образ после освобождения Docker storage"
status: done
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-26
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: scripts/dsh-bump.sh --update перенёс Dockerfile на 0.2.1-rc.2, но make img_build упал на docker layer extraction: no space left on device (Docker Desktop build cache 53.85 GB; host Data volume 94%). Pin и default tag возвращены к 0.2.0-rc.2 / agctl-dsh:0.2.0; новую версию не считать верифицированной и не использовать до e2e.
- 2026-10-19: Попытка make img_build с DSH 0.2.1-rc.2 завершилась статусом 2: Docker Desktop exporter не смог распаковать слой (no space left on device, build cache 53.85 GB). Dockerfile pin и default tag возвращены на 0.2.0-rc.2 / agctl-dsh:0.2.0; image tag 0.2.1 не использовать до проверки. Не запускать docker system prune без владельца.
- 2026-10-26: 2026-10-26: причина старого сайдбара найдена и устранена — образ agctl-dsh:0.2.0 содержал dsh 0.2.0-rc.2, чей фронтенд не имеет Workspaces-браузера (плоский «Сессии»-список). Dockerfile поднят на @deepseek-ai/dsh@0.2.1-rc.2, тег образа → agctl-dsh:0.2.1 (img.mk + дефолт --image в serve). Освобождён build-cache (14.4GB, builder prune), образ собран и проверен: dsh 0.2.1-rc.2, в dsh-client-ui-workspace есть Add workspace/Group by/Workspace Tree, строки «Сессии» нет. Для применения владельцу: make stop && make up (новый дефолт тега), затем docker rm -f контейнера чата — пересоздастся на 0.2.1.
- 2026-10-26: Финал: образ agctl-dsh:0.2.2 (dsh 0.2.1-rc.2) собран и развёрнут; визуальная проверка headless-браузером — нативный Workspaces (папки, поиск, фильтры, Add workspace), чип профиля над Settings. Попутно найден и удалён главный виновник старого вида: chatshell-инъекция шлюза (internal/server/chatshell.go) прятала нативные кнопки Workspaces и переименовывала заголовок в «Сессии» (2f243fa).
- 2026-10-26: done — dsh 0.2.1-rc.2 в agctl-dsh:0.2.2; вид проверен визуально
