---
schema: 1.0.0
id: docker-sandboxes-local-runtime
title: "Локальный режим: Docker Sandboxes (sbx) как альтернативный рантайм рядом с docker"
status: open
priority: P2
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-20
updated: 2026-11-20
tags: ["local", "sandbox", "docker", "security"]
depends: []
---
## Описание

Docker Sandboxes (CLI `sbx`, преемник удалённой в Docker 29.x команды
`docker sandbox`) — microVM на песочницу: своё ядро и свой dockerd,
папка проекта по virtiofs на том же пути, весь исходящий TCP через прокси
хоста с политикой «запрещено по умолчанию», подстановка ключей на прокси
(агент видит заглушку), MCP-шлюз на хосте, свои образы как kit (OCI-образ +
YAML). Локально бесплатно, в том числе коммерчески.

Это закрывает три пробела локального режима: песочница dsh (Landlock) не
работает на bind-маунтах Docker Desktop и entrypoint уходит в
`danger-full-access` (карточка `sandbox-deny-after-clone`); ключ модели
читается агентом из `.credentials.yaml` (architecture §8.4); egress-allowlist
не включается из CLI.

**Обычный docker не выпиливаем.** `sbx` — альтернативный рантайм рядом с
`dockerrt`: по умолчанию остаётся `docker`, `sbx` включается явно
(`dev.yaml` / флаг `agctl dev init|up --runtime sbx`), откат — сменой
значения без потери данных.

Сначала спайк (проверки по порядку, любая «нет» по п.1–2 останавливает):
1. Вход `sbx login` (Docker OAuth, обязателен) из сети ЦИТ и допустимость
   Docker-аккаунта по политике организации; доступ к `login.docker.com`,
   Docker Hub.
2. Платформы разработчиков: macOS 14+ только Apple Silicon (Intel нет),
   Windows 11 + Windows Hypervisor Platform (отдел 1С), Ubuntu 24.04+ с KVM.
3. Образ `agctl-dsh` как kit-workload: долгоживущий `dsh web` на 3080,
   проброс `sbx ports --publish` на 127.0.0.1, домашний каталог dsh.
4. Работает ли Landlock dsh на virtiofs внутри VM (может сломаться так же).
5. Подстановка ключа для LiteLLM организации (собственный base URL), а не
   только встроенных провайдеров `sbx secret set <provider>`.
6. Мост к хосту: `agctl bridge mcp` как stdio MCP-сервер на хосте через
   MCP-шлюз sbx, либо политика на `host.docker.internal:<порт>`.

Затем бэкенд `internal/runtime/sbxrt` (контракт `ChatProvider`) через CLI
`sbx` — локального API нет (REST API/TS SDK только для облачных песочниц,
облако для ЦИТ не рассматриваем: данные уходят из организации).

Источники: https://docs.docker.com/ai/sandboxes/ (architecture, install,
customize, faq).

## Критерии готовности

- [ ] Спайк: отчёт по п.1–6 в журнале карточки, решение «делаем / нет».
- [ ] `--runtime docker|sbx` в локальном режиме, по умолчанию `docker`;
      поведение и тесты режима `docker` не меняются.
- [ ] `sbxrt`: старт/стоп/сон, проброс порта, папка проектов, дом dsh;
      `agctl dev doctor` проверяет `sbx`, вход и KVM/WHP.
- [ ] Ключ модели не попадает в VM (заглушка + подстановка на прокси),
      политика сети по умолчанию — провайдер модели, GitLab, Jira из профиля.
- [ ] Мост к хосту работает в обоих рантаймах.
- [ ] README и `docs/architecture.md` (§6) описывают оба рантайма и выбор.

## Журнал

- 2026-11-20: создано (`gitctl backlog new`). Исследование: на машине
  владельца Docker 29.9.3 — `docker sandbox` удалён, перенаправляет на sbx.
  Владелец: обычный docker остаётся, sbx — альтернативный режим рядом.
