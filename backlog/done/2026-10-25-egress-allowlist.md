---
schema: 1.1.0
id: egress-allowlist
title: "Egress-allowlist контейнеров: сеть только до LLM/хоста (opt-in флаг)"
status: done
priority: P2
size: L
area: infra
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-11-12
tags: []
depends: []
epic: two-mode-mvp
sessions:
  - session-f61b3c76-c5b1-4184-b046-86a58e758c75:work
---
## Описание

Контейнеры сегодня имеют неограниченный egress. Opt-in флаг agctl:
выделять сеть agctl и ставить DOCKER-USER правила — наружу только DNS и
host-gateway (LiteLLM). Решение владельца об умолчании — отдельно.

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-11-12: Реализовано как opt-in флаг --egress-allowlist / serve.egress_allowlist (default off; умолчание — отдельное решение владельца, как и в карточке). Критерии, по которым закрыто: (1) контейнер агента на выделенной сети agctl-egress-<hash дома>; (2) egress из подсети: DNS udp/tcp 53 разрешён, established/related разрешён (публикация портов работает), gateway сети разрешён (LLM на хосте; на Linux контейнер→gateway идёт INPUT мимо DOCKER-USER, ACCEPT прикрывает Docker Desktop-форвард), остальное DROP; (3) правила идемпотентны, per-home цепочка AGCTL-EGRESS-<hash> — несколько agctl на одном демоне не конфликтуют; (4) применение — хелпер --network host + NET_ADMIN из того же образа (iptables добавлен в Dockerfile) — работает и на Linux-хосте, и в Docker Desktop VM. Проверено живьём на демоне: getent OK, внешний HTTPS DROP (timeout), HTTP на gateway сети отвечает. Кавеат dev-хостов: магический host.docker.internal Docker Desktop (IPv6 vpnkit-адрес) в этом режиме недостижим — на Linux-проде gateway = сам хост; внешние LLM-endpoint'ы требуют маршрутизации через хост (LiteLLM), это by design флага. k8s-провайдер не покрыт (docker-only). Источник — код репо + живые прогоны docker; карточка тела почти не имела (критерии «сформулировать»), контекст восстановлен из docs/secure-gateway.md и dockerrt.
- 2026-11-12: refl: Карточка пришла без критериев (чернового качества, до LLM-гейта) — взял с формулировкой критериев в журнале. Найдена датозависимая поломка TestProxyRequestsActivity (окно активности), живущая и на main — кандидат в бэклог.
- 2026-11-12: done — Opt-in egress-allowlist: выделенная сеть + DOCKER-USER правила (DNS/gateway/established — да, остальное DROP), флаг default off; проверено живым демоном
