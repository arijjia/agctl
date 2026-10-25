---
schema: 1.0.0
id: container-healthcheck-canary
title: "Healthcheck и relay-канарейка: убирать полумёртвые контейнеры"
status: done
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

Контейнер места — 2–3 процесса без супервайзора: relay (`:3080`),
dsh web (`:3099`), sshd (`:2222`, опционально). Если dsh web падает, relay
продолжает слушать — контейнер «полумёртвый»: выглядит живым, сервиса нет.
Healthcheck нет; `RestartPolicy: no` (`internal/runtime/dockerrt/docker.go:298`)
осознан (контейнер расходный по модели TTL), но долгоживущие dev-места от
этого страдают.

## План

1. HEALTHCHECK в Dockerfile: probe `127.0.0.1:3099` внутри контейнера
   (node-однострочник, curl уже в образе).
2. Relay-канарейка (`internal/assets/image/relay.mjs`): если upstream
   `127.0.0.1:3099` отказывает N секунд/подряд K соединений — `process.exit(1)`;
   контейнер умирает целиком (PID1 = dsh уже мёртв или помрёт), полумёртвого
   состояния больше нет.
3. Пересоздание остаётся за существующими механизмами: `/chat/` пересоздаёт
   среду лениво (`chat.go ensureChat`), dev-место — кнопка Start/`make up`
   (`handleWorkspaceStart`); решить, нужен ли auto-restart для dev-мест
   отдельно от TTL-модели чата.

## Не делать

- Не переводить контейнеры на постоянный `restart=always`: ломает модель
  «рантайм — расходник, том — персистентный» и логику TTL-сна чата.

## Критерии готовности

- E2E: `docker exec … kill <pid dsh>` → контейнер умирает/уходит unhealthy →
  обращение к `/chat/` (или Start dev-места) пересоздаёт среду с целым
  томом; сессии и файлы на месте.
- `docker ps` показывает health-статус; ложных unhealthy на живом dsh нет.
- Доки: `docs/architecture.md` §13/§14 упоминания healthcheck/канарейки.

## Журнал

- 2026-10-25: создано (`gitctl backlog new`). Идея — дискуссия с владельцем
  «контейнер на процесс» (agctl-hq/docs/v3/architecture-explained.md §14):
  выбран вариант «статус-кво + канарейка», вариант K8s Pod — в карточках
  k8s-*-adapter.
- 2026-10-25: done — HEALTHCHECK :3099 + reconcile unhealthy вошли в волну A (main)
