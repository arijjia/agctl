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
updated: 2026-11-22
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
- 2026-11-22: Часть 1 (egress) сделана в ветке agent/dockerrt-egress-flag. Флаг терялся при разделении бинарей 2026-11-19 (Config.EgressAllowlist был, serve не выставлял) — возвращён в общий serve: --egress-allowlist / AGCTL_EGRESS_ALLOWLIST / serve.egress_allowlist, --egress-allow / AGCTL_EGRESS_ALLOW / serve.egress_allow, профиль egress.allow, dev.yaml egress_allowlist + egress_allow (agctl dev передаёт явными флагами). Правила: кроме DNS/шлюза — IPv4 провайдеров модели (те же пины /etc/hosts), белый список (хосты пинятся, IPv4, CIDR), объединение по дому. Найдено и исправлено: на Docker Desktop host-gateway = 192.168.65.254 (не шлюз сети) — мост к хосту и LiteLLM на хосте из egress-сети были недоступны; хелпер теперь узнаёт IPv4 host-gateway и разрешает его (негативный контроль: без правила host.docker.internal закрыт). Проверки на Docker Desktop: TestEgressLive (1.1.1.1 открыт, 9.9.9.9 закрыт, сервис на хосте открыт), EGRESS=1 e2e-local-mode 47/0 (мост из egress-сети), e2e-local-mode 44/0, e2e-smoke с AGCTL_EGRESS_ALLOWLIST=1 40/0 CHAT=PASS (стаб модели на хосте), без egress 40/0. Не проверено: Linux-хост. Осталось: часть 2 — прокси с подстановкой ключа модели.
