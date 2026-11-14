---
schema: 1.0.0
id: local-mode-v2-container-bridge
title: "Локальный режим v2: контейнер + мост к хосту (agctl dev)"
status: open
priority: P1
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-13
updated: 2026-11-14
tags: ["local", "dev", "docker", "bridge"]
depends: ["local-dev-mode"]
---
## Описание

Пересмотр развилки из `local-dev-mode` (2026-11-01): **основной движок
локального режима — контейнер, нативный dsh — запасной**. Приоритеты
сменились: воспроизводимость у нескольких команд разработчиков, «внутри
образа всё установлено», кроссплатформенность, минимум поддержки зоопарка
машин. Доступ агента к машине — маунты выбранных папок, сеть хоста и мост
к хосту. Концепция и план пилота — `agctl-hq/docs/local-mode-concept-2026-11-13.md`,
вопросы к организации — `agctl-hq/docs/local-mode-questions-2026-11-13.md`.
Решение «нативный режим основной» из `local-dev-mode` этой карточкой
заменяется.

Состав:
- `agctl dev init/up/down/status/doctor` поверх `serve`: один пользователь
  (developer), только loopback, без регистрации; контейнер на локальном
  Docker (dockerrt), маунт выбранной папки проектов.
- Разработчик получает бинарь `agctl` + готовые образы из реестра
  организации (не клон репо и не `make up`).
- Секреты в хранилище ОС (Keychain / Credential Manager / Secret Service):
  `agctl dev secrets`, мастер «Доступы» в Кабинете, env-инжект в контейнер
  по манифесту (заменяет `AGCTL_DEV_ENV` + `.env`). Ключ LLM на пилоте —
  вручную.
- Мост к хосту: `agctl dev` отдаёт контейнеру MCP-сервер с белым списком
  хост-команд (например проверка конфигурации платформой на Windows-хосте),
  журнал вызовов, подтверждение опасных.
- `agctl dev update/rollback`: pull образа по digest из профиля → бэкап тома
  dsh-home → перезасев управляемых слоёв → проверка созданием сессии →
  откат при провале; блок «Обновления» в Кабинете. Идеи — из dsh-mgmt
  (кэш+SHASUMS, INSTALL→ADAPT→VERIFY→RECORD, бэкап-гейт).
- Вкусы образов через надстройки профиля: базовый (+glab, jira-cli, MCP
  Jira/GitLab), web (+Playwright, браузеры, MCP Playwright), 1С (+Java,
  BSL Language Server, OneScript).

## Критерии готовности

- [x] На чистой машине с Docker: `agctl dev init` + `agctl dev up DIR` →
      брендированный dsh, агент правит файлы в DIR; данные переживают
      рестарт и обновление.
- [x] Секреты GitLab/Jira/LLM из хранилища ОС доходят до контейнера, не
      попадают в образ, логи и git.
- [ ] Образы web и 1С публикуются в реестр; разработчик ничего не собирает.
      (образы собираются `make img_build PROFILE=…`, пушатся `make img_push`;
      ждёт адреса Nexus и ссылок на него в images.dev профиля)
- [x] `update` и `rollback` проходят без потери сессий; e2e-скрипт.
- [ ] Проверено на Windows (WSL2) и macOS. (macOS — живьём; Windows —
      только кросс-сборка, нужна машина)
- [x] Мост к хосту: минимум одна хост-команда из белого списка вызывается
      агентом с журналом.

## Журнал

- 2026-11-13: создано (`gitctl backlog new`). Концепция и вопросы — в штабе (docs/local-mode-*-2026-11-13.md). Известно: реестр образов — Nexus организации, ключи LLM на пилоте вручную, Docker на машинах тестовых групп ставит организация; открыт вопрос про WSL2.
- 2026-11-14: реализовано на ветке agent/local-mode-v2. `agctl dev init/up/down/status/open/logs/secrets/bridge/update/rollback/doctor` поверх serve (Config.Local: владелец developer, вход по одноразовому токену, без регистрации, loopback; /workspace = папка разработчика, автосейв и удаление каталогов выключены). Секреты: go-keyring (Keychain/Credential Manager/Secret Service) с запасным AES-GCM файлом; env контейнера + производные имена для glab/MCP. Найдено: bash dsh вычищает *TOKEN*/*KEY*/*SECRET*/*PASSWORD* из env команд агента — доступы теперь ещё и файлами в dsh-home/.agctl (git credential helper, glab config, credentials.env, bridge.json; указатели GIT_CONFIG_GLOBAL/GLAB_CONFIG_DIR/AGCTL_CREDENTIALS_ENV), касается и серверного DevEnv. Мост: internal/localbridge (argv без shell, проверка значений, confirm + approve на хосте, журнал), клиент `agctl bridge list|call|mcp` в контейнере, скилл agctl-host-bridge, MCP «Мост к хосту» (только локально); на Linux — доп. слушатель на docker0 только для путей моста. Образы: базовый + glab/jira-cli/uv/rg/jq/клиенты БД, прогрев MCP GitLab/Playwright; вкус web (Playwright+Chromium) в ядре; вкус 1С (Temurin JRE 21 + BSL LS 1.0.7 — 1.0.x не работает на Java 17) и белый список ibcmd — в профиле utmn. Каталог MCP: + GitLab, Jira (uvx mcp-atlassian), Playwright, мост. Попутно: Windows-сборка main падала на agent-substrate/env/guest — вынесено в axguest_unix.go. Проверки: go vet/test, -race на сервере, bin/e2e-local-mode.sh 42/42 на живом Docker (macOS), агент в чате сам вызвал команду хоста через мост, MCP Playwright открыл страницу в образе web. Осталось: адреса Nexus в профиле, Windows/WSL2 живьём, Jira-MCP живьём на Jira ЦИТ, jira-cli init.
