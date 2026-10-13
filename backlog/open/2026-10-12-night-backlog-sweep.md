---
schema: 1.0.0
id: night-backlog-sweep-2026-10-12
title: "Ночная смена 2026-10-12: закрыть бэклог agctl волнами с максимальным тестированием"
status: in-progress
priority: P1
size: L
area: meta
repo: arijjia/agctl
created: 2026-10-12
updated: 2026-10-13
tags: [night, epic]
depends: []
---
## Описание

Видение владельца (дословно): «Пожалуйста сейчас за ночь постарайся закрыть
все бэклоги по agctl. Тестируй обязательно по максимуму».

Карточки контрактов живут в `agctl-hq/backlog/open/`; инженерная реализация —
здесь, по ветке на задачу. Честный статус каждой карточки hq — в её журнале;
не закрывать то, что не проверено живьём.

## План (волны)

- W1 `dsh-pin-bump-automation`: пин DSH 0.1.7-rc.2 → 0.2.0-rc.2, образ,
  изолированный e2e-смоук по гайду ЦИТ, скрипт сверки версии с npm.
- W2 `secure-dsh-gateway`: авторизованный прокси `/w/{id}` (HTTP+WS) вместо
  редиректа на loopback-порт; тесты межпользовательского доступа; cookie
  вместо токена в URL — насколько позволяет механика dsh; threat-model в доке.
- W3 `workspace-component-packages`: манифест стартовых комплектов, compose
  в provision, API + галочки в UI (all-on по умолчанию), тесты.
- W4 `runtime-mlops-capacity`: замеры RAM/CPU idle/active, холодный/тёплый
  старт на тихой машине, таблица результатов.
- W5: дизайн-спеки для hq-карточек (knowledge-contract, catalog-манифест,
  baseline-артефакты ВКР).

## Критерии готовности

- Каждая закрытая карточка: код + тесты (go test/vet, UI-сборка, e2e-живой
  провод) + коммит в своей ветке + журнал в hq-карточке.
- Не закрытое — честно помечено с причиной (внешняя зависимость/объём).

## Журнал

- 2026-10-12: эпик создан, 0-волна: backlog/ развёрнут в agctl, AGENTS.md
  с правилом рефлексии; разведка: docker 29.8.2, node 25.2.1, npm ok, агctl
  чист на main, посторонних agctl-контейнеров нет, порт 18929 свободен.
- 2026-10-13: W1: DSH 0.2.0-rc.2 pin, dsh-bump update/check + weekly e2e-gated PR, isolated e2e PASS 35/0 CHAT=PASS. W2: gateway /w/{id} auth, mint, WS/SSE, Secure cookies, e2e 35/0; hq secure-gateway выполнена. Legacy dsh-home 0.1.7→0.2.0 migration ещё не проверена.
