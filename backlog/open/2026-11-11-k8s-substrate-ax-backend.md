---
schema: 1.0.0
id: k8s-substrate-ax-backend
title: "Kubernetes-бэкенды k8s и ax (Google AX + Agent Substrate): хвосты после первой волны"
status: open
priority: P2
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-11
updated: 2026-11-11
tags: [kubernetes, substrate, ax, runtime]
depends: []
epic: ""
---
## Описание

Первая волна (ветка agent/k8s-substrate-ax-rshb) дала `agctl serve
--runtime k8s|ax`: k8srt доведён до рабочего (readiness, замена подов по
версии/образу, SSH через NodePort, общий RWX-том со шлюзом, health, exec для
SSH-lease, свип дрейфа), axrt — адаптер поверх gRPC ax-server и
atenet-router с раннером `agctl ax-runner` в образе, манифесты `deploy/k8s`
(base, kind, ax), образ шлюза, make-цели, kind-смоук, `docs/kubernetes.md`.
Live-прогон на настоящем кластере (а тем более с Substrate/AX) в этой волне
не делался: на машине разработчика нет Docker/kind. Карточка — что осталось
до «production K8s заявляется».

## План

1. Прогнать `make k8s_e2e_kind` на хосте с Docker/kind; починить, что
   всплывёт (subPath на пустом RWX, fsGroup, readiness-тайминги dsh).
2. Live-валидация ax: kind + Substrate + AX по их README, WorkerPool
   `deploy/ax/workerpool.yaml`, `make k8s_apply K8S_OVERLAY=overlays/ax`;
   проверить маршрутизатор (http :80 и mTLS :443), boot-log через раннер,
   suspend/resume с сохранением `/workspace`.
3. Egress-инъекция ключей LLM: agctl сам создаёт `EgressPolicy` актора
   (CreateActorEgressPolicy) из профиля при `--ax-inject-llm-key=0`, чтобы
   ключ никогда не попадал в песочницу.
4. Неизменяемость задачи AX: обновление провайдера без пересоздания
   (раннер забирает конфиг провайдера у шлюза при каждом boot по токену
   раннера) — снимет предупреждение о дрейфе и потерю `/workspace`.
5. SSH в режиме ax: либо guest-сервисы Substrate (`spec.debug`) и
   `ax ssh`, либо sshd через маршрутизатор (TCP-туннель) — решение владельца.
6. Egress-политика подов k8s (opt-in allowlist LLM-хостов), HA control-plane
   (PostgreSQL + leases) — отдельные карточки по роадмапу.

## Критерии готовности

- kind-смоук k8s зелёный в CI (или в ночном прогоне на хосте с Docker).
- Пилотная установка в режиме ax: пользователь входит, под/актор
  поднимается, засыпает по TTL и просыпается с сохранёнными проектами.
- Ключи LLM в режиме ax не хранятся в env задачи (egress-инъекция).

## Не делать

- Не форкать AX/Substrate; версии пиновать через go.mod (`github.com/google/ax`).
- Не делать Pod/актор на проект — по-прежнему один рантайм на пользователя.

## Журнал

- 2026-11-11: создано по итогам первой волны (k8srt/axrt/ax-runner, deploy/k8s, docs/kubernetes.md).
