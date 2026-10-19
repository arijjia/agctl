---
schema: 1.0.0
id: dsh-image-021-rc2-rollout
title: "Накатить DSH 0.2.1-rc.2 и пересобрать образ после освобождения Docker storage"
status: open
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-19
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
