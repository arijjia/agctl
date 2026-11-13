---
schema: 1.5.0
id: k8s-pod-egress-policy
title: "Egress-политика подов k8s: opt-in allowlist LLM-хостов"
status: open
priority: P3
size: M
area: runtime
repo: arijjia/agctl
created: 2026-11-14
updated: 2026-11-14
tags: ["kubernetes", "security", "network"]
depends: [egress-allowlist]
sessions:
  - session-c780be29-3c20-4064-a46e-9f9eb1cb8b92:work
---

## Описание

Поды пользователей в режиме `--runtime k8s` (internal/runtime/k8srt) имеют
неограниченный исходящий трафик: NetworkPolicy в deploy/k8s ограничивает
только ingress к dsh. URL LLM задаёт пользователь/профиль — egress кластера
остаётся за оператором (docs/kubernetes.md §2). Проблема: скомпрометированная
сессия dsh может сливать данные на любые хосты; для инсталляций с повышенными
требованиями нужен opt-in режим, где под чата ходит наружу только на
разрешённые LLM-эндпоинты (+ DNS).

Вводные из уже сделанного:
- ChatSpec.EgressAllowlist (карточка egress-allowlist, docker-режим):
  контейнер на выделенной сети, egress только DNS — семантика переиспользуется.
- Провайдеры LLM известны шлюзу: spec.Provider.AllBaseURLs() + Extras[].BaseURL
  — из них выводится список hostnames.
- В режиме ax аналогичную роль играет EgressPolicy актора Substrate
  (automated, карточка k8s-substrate-ax-backend п.3) — тут паритет для k8s.

## План

1. Механизм фильтрации выбран и зафиксирован: NetworkPolicy egress с
   ipBlock-листом (работает на любом CNI): allow DNS (udp/tcp 53 к
   kube-dns) + ipBlock CIDR разрешённых LLM-хостов; остальное deny. Шлюз
   периодически DNS-resolve’ит hostnames провайдеров из ChatSpec
   (spec.Provider.AllBaseURLs() + Extras[].BaseURL) и обновляет список
   политики. Ограничение ipBlock (динамические IP у LLM-провайдеров) —
   принять на v1 и явно описать в docs; L7-прокси/Cilium FQDN — отдельная
   карточка в будущем, если v1-ограничение станет проблемой эксплуатации.
2. Opt-in флаг serve `--k8s-egress-policy` (env
   `AGCTL_K8S_EGRESS_POLICY`, config `serve.k8s.egress_policy`), off
   по умолчанию.
3. Тесты: юнит на генерацию политики из ChatSpec; kind-смоук
   (bin/e2e-k8s-kind.sh) с фейковым LLM-хостом: разрешённый хост отвечает,
   посторонний — блокируется.

## Критерии готовности

- kind-смоук с включённой политикой зелёный: чат работает с LLM-эндпоинтом,
  curl на посторонний хост из пода блокируется.
- Флаг off по умолчанию: поведение существующих инсталляций не меняется.

## Не делать

- Не трогать ingress-политики шлюза; не делать allowlist для docker-режима
  (отдельная карточка egress-allowlist).

## Журнал

- 2026-11-13: создано из п.6 карточки k8s-substrate-ax-backend (draft, тело из роадмапа docs/architecture.md §6 + docs/kubernetes.md).
- 2026-11-14: создано (`gitctl backlog new` --draft)
- 2026-11-14: gate: not-ready (попытка 1) — причина в gate-feedback блоке
- 2026-11-14: gate: ready (попытка 2)
- 2026-11-14: gate passed, feedback cleared
- 2026-11-14: unpark: тело довнесено, gate ready (попытка 2)
