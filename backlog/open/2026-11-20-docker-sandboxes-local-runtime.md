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
updated: 2026-11-26
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
- 2026-11-26: реализация без живого прогона (ветка `agent/sbx-runtime`).
  **Живого sbx не было**: на машине разработки (macOS 15.7, arm64, Docker
  29.9.3) `sbx` не установлен; установка требует доверить tap `docker/tap`
  и войти под учётной записью Docker (п.1 спайка — решение владельца), поэтому
  не ставился. CLI изучен по справочнику docs.docker.com/reference/cli/sbx
  (docker/docs `data/sbx_cli`, снимок 2026-11-25) и руководствам
  `ai/sandboxes` — каждый использованный флаг оттуда, список в шапке
  `internal/runtime/sbxrt/sbxrt.go`. Карточка остаётся открытой.

  Сделано и проверено тестами с подделкой CLI (тестовый бинарь в роли `sbx`):
  - `internal/runtime/sbxrt`: старт, сон (`sbx stop`), пробуждение без
    пересоздания, пересоздание при смене образа/маунтов/ресурсов/правил сети,
    порт dsh web на `127.0.0.1`, дом dsh и папки проектов маунтами, логи и
    здоровье по процессу `sbx exec`, явная ошибка с хвостом лога, если dsh не
    поднялся;
  - ключ модели: в VM — заглушка (`.credentials.yaml`, окружение), значение —
    `sbx secret set-custom --value-stdin`; сеть: провайдеры модели, URL из
    окружения места (GitLab, Jira, мост → `localhost:<порт>`), `egress.allow`;
  - выбор: `runtime` в `dev.yaml`, `agctl dev init|up --runtime`, строка в
    `status`, проверки в `doctor` (установлен, вход, Apple Silicon / `/dev/kvm`;
    WHP на Windows отсюда не проверяется — ошибку даст сам sbx), лаунчер:
    `GET /api/local/ui` (`runtimeKind`, `runtimes`, `runtime.kind`) и
    `PUT /api/local/ui/runtime`; интерфейс SPA — отдельной задачей;
  - режим `docker` не менялся: по умолчанию он, sbx не трогается, пока не
    выбран.

  Спайк, что осталось проверить на живом sbx (по пунктам описания):
  1. Вход и политика организации — не проверялось.
  2. Платформы — по документации: macOS 14+ Apple Silicon, Windows 11 + WHP,
     Ubuntu 24.04+ с KVM; проверка в `doctor`.
  3. Образ `agctl-dsh` как шаблон — главный риск. Документация требует в
     образе пользователя `agent` (UID 1000, дом `/home/agent`), наш образ
     работает от root с домом `/home/dsh` (а UID 1000 в `node:22-bookworm-slim`
     занят пользователем `node`). Сейчас образ отдаётся как есть
     (`--template`, агент `shell`, запуск `sbx exec -u root`); если sbx его не
     примет — нужен производный образ-шаблон или kit v3. Также не проверено,
     что `sbx template load` принимает tar от `docker save` и что
     `agctl-dsh-net print` находит адрес интерфейса внутри песочницы.
  4. Landlock dsh на virtiofs — не проверялось; детект в entrypoint тот же.
  5. Подстановка ключа для собственного LiteLLM (`secret set-custom` с хостом
     организации, в т.ч. по HTTP и с корпоративным CA) — не проверялось.
  6. Мост — через `host.docker.internal` и правило `localhost:<порт>` (по
     руководству «Accessing host services from a sandbox»); MCP-шлюз sbx не
     использован.
