---
schema: 1.0.0
id: runtime-matrix-decision
title: "Матрица рантаймов: Docker-совместимые движки в doctor, живой спайк sbx, оценка Apple container"
status: open
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-27
updated: 2026-11-27
tags: ["local", "runtime", "sandbox"]
depends: []
---
## Описание

Решение по рантаймам (`docs/architecture.md` §13.2): `docker` — основной на
любом Docker-совместимом движке, `sbx` — опция после живого спайка,
отдельного адаптера containerd не делаем, Apple `container` (macOS 26,
microVM на контейнер, без учётной записи Docker) — кандидат.

## Критерии готовности

- [ ] `doctor` распознаёт Docker Desktop, Docker Engine, Colima, Rancher Desktop (moby), Podman с Docker API; e2e-local-mode хотя бы на двух.
- [ ] Живой спайк sbx по пунктам карточки `docker-sandboxes-local-runtime` (вход из сети организации, образ как шаблон, LiteLLM).
- [ ] Короткий отчёт по Apple `container`: образ `agctl-dsh`, маунты, порт, сеть; решение «делаем адаптер / нет».

## Журнал

- 2026-11-27: создано (`gitctl backlog new`) по итогам исследования Docker AI Governance, рантаймов и фоновых агентов.
