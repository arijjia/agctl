---
schema: 1.0.0
id: dockerrt-egress-flag-key-proxy
title: "Docker-бэкенд: флаг egress-allowlist в serve и прокси с подстановкой ключа модели"
status: open
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-20
updated: 2026-11-20
tags: ["docker", "security", "secrets", "egress"]
depends: []
---
## Описание

Два механизма Docker-бэкенда (локальный и серверный режимы), нужные
независимо от Docker Sandboxes:

1. **Egress-allowlist из CLI.** `dockerrt/egress.go` (выделенная сеть +
   цепочка в `DOCKER-USER`) реализован и покрыт тестом, но флага в `serve`
   нет (`Config.EgressAllowlist`, architecture §4.2, §10.4). Добавить флаг
   `serve --egress-allowlist` и настройку в `dev.yaml`; список разрешённых
   хостов — из профиля (провайдер модели, GitLab, Jira, реестр).
2. **Прокси с подстановкой ключа модели.** Сейчас ключ лежит в
   `.credentials.yaml` и агент может его прочитать (§8.4); скрывает ключ
   только AX с `--ax-inject-llm-key=0`. Сделать на хосте прокси к
   провайдеру модели: в контейнер — заглушка и base URL прокси, настоящий
   ключ подставляет прокси (та же модель, что у sbx и AX).

## Критерии готовности

- [ ] Флаг egress-allowlist в `serve` и `agctl dev`, e2e: запрещённый хост
      недоступен, провайдер модели доступен, на Linux и Docker Desktop.
- [ ] Прокси ключа: в `.credentials.yaml` и окружении контейнера нет
      настоящего ключа; чат работает; ключ не попадает в логи.
- [ ] `docs/architecture.md` §4.2, §8.4 обновлены.

## Журнал

- 2026-11-20: создано (`gitctl backlog new`) по итогам исследования Docker
  Sandboxes.
