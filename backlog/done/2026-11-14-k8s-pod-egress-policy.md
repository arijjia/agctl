---
schema: 1.5.0
id: k8s-pod-egress-policy
title: "Egress-политика подов k8s: opt-in allowlist LLM-хостов"
status: done
priority: P3
size: M
area: runtime
repo: arijjia/agctl
created: 2026-11-14
updated: 2026-11-15
tags: ["kubernetes", "security", "network"]
depends: [egress-allowlist]
sessions:
  - session-c780be29-3c20-4064-a46e-9f9eb1cb8b92:work
  - session-1247c577-a767-45f9-80c3-6e31877ad29a:work
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
- 2026-11-15: Реализовано по плану: egress-NetworkPolicy netegress-<runtime> чат-пода (DNS udp/tcp 53 к kube-dns + ipBlock /32 resolved-хостов провайдеров через AllProviderURLs), opt-in --k8s-egress-policy / AGCTL_K8S_EGRESS_POLICY / serve.k8s.egress_policy (off по умолчанию), DNS-ресолв hostname'ов из ChatSpec при EnsureChat + фоновый рефрешер каждые 10 мин (динамические IP), StopChat снимает политику и снимает чат с рефрешера, полный провал ресолва — громкий отказ EnsureChat. Юниты: hostnames/extraction, генерация политики, сравнение правил, lifecycle (create/update/удаление) на fake clientset, loud-fail.
- 2026-11-15: kind-смоук зелёный (EGRESS=1 make k8s_e2e_kind, Calico v3.29.2 вместо kindnet — тот не энфорсит NetworkPolicy): чат поднимается с политикой, curl из пода dsh на разрешённый fake-llm (headless svc, IP в ipBlock) — HTTP 200, на pod IP шлюза — блокировка по таймауту, StopChat удаляет политику вместе с compute, PVC цел. Извлечено из источников и перенесено: ограничение kindnet (не энфорсит политики) и паттерн headless-svc для ipBlock — в e2e-скрипт и docs/kubernetes.md §2; flag-семантика согласована с AGCTL_K8S_* конвенцией.
- 2026-11-15: refl: refl: NetworkPolicy-смоук потребовал policy-CNI в kind — Calico ставится поверх кластера без штатного CNI (disableDefaultCNI); pod-спека fake-хоста сразу упёрлась в pod-security restricted namespace (non-root + drop ALL + RuntimeDefault) — полезный якорь для будущих e2e-подов. Обнаруженный при первом прогоне ложнозелёный чек (curl 000 + || echo blocked склеивались) починен на честную пару rc+http_code — паттерн для exec-проверок с curl.
- 2026-11-15: done — Карточка закрыта: opt-in egress-политика чат-подов k8s реализована и проверена живым kind-смоуком (Calico): DNS+ipBlock LLM-хостов, рефрешер, удаление по StopChat, off по умолчанию. Критерии готовности выполнены: смоук EGRESS=1 зелёный (разрешённый хост 200, посторонний блокируется), поведение инсталляций без флага не меняется. L7/FQDN-фильтрация — за будущей волной при потребности (описано в docs).
