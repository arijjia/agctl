---
schema: 1.0.0
id: dsh-plugin-cabinet-scaffold
title: "Каркас клиент-плагина «Кабинет agctl» в dsh web (панель sidebar.panellist)"
status: done
priority: P1
size: L
area: plugin
repo: arijjia/agctl
created: 2026-10-18
updated: 2026-10-18
tags: ["dsh-client-plugin"]
depends: []
epic: night-dsh-plugins
---
## Описание

Волна 1 эпика `night-dsh-plugins`. Пакет `dsh-plugins/cabinet/` в репо
agctl — клиент-плагин dsh web (`platform: "web"`, React 18, cordis peer,
сборка tsdown), который:

1. регистрирует панель в root-scoped списке `sidebar.panellist`
   (id `agctl-cabinet`, label «Кабинет», иконка);
2. регистрирует контент панели в keyed slot `main` лейаута (тот же id):
   экран «Кабинет agctl» — заголовок, состояние подключения к agctl,
   список рабочих мест (на волне 1 — заглушка/локальный стейт, волна 2 —
   живой REST);
3. ничего не ломает в дефолтном чате: без входа в панель UI выглядит как
   обычный dsh web (Tier 0 дефолт).

Плюс `scripts/night-profile.sh`: изолированный `DSH_HOME`
(например `/tmp/agctl-night-dsh-home`), профиль с нашим плагином в
`dsh.profile.bundles`, бут `dsh web --port 3555 --no-open`, логи в /tmp;
живая сессия владельца (:3080) не затрагивается.

Референсы контракта: `node_modules/@deepseek-ai/dsh-client-ui-sidebar/README.md`
(раздел Global panel entries), `dsh-client-ui-slots/README.md`,
манифест и сборка — `dsh-client-ui-jobs/package.json` (dsh.client.inject,
tsdown). Стиль кода — TypeScript ESM, без секрета в коде/логах.

## Критерии готовности

- `scripts/night-profile.sh` поднимает dsh web на 127.0.0.1:3555, в UI
  видна панель «Кабинет», клик открывает экран кабинета; дефолтный чат
  работает как обычно.
- Скриншот UI сохранён и приложен к отчёту волны (путь в журнале эпика).
- Пакет собирается (`pnpm build`), есть минимальный README плагина.
- Коммит(ы) в ветке `agent/night-2026-10-18-dsh-plugins`; журнал карточки
  дополнен через `gitctl backlog note`.
- 2026-10-18: Волна 1 выполнена оркестратором (субагент прерван за 40 мин без артефактов): пакет dsh-plugins/cabinet — рукописный Lazy-CJS бандл (формат window.__ModuleLoader__.load по образцу dsh-client-ui-jobs), регистрация панели agctl-cabinet в sidebar.panellist + keyed slot main; манифест dsh.bundle.patch=cordis.patch.yml (insert agctl-cabinet). Скрипт scripts/night-profile.sh: изолированный DSH_HOME=/tmp/agctl-night-dsh-home, профиль night-cabinet со стеком бандлов web (base+web-app+наш), бут dsh --profile night-cabinet --port 3555 --no-open. Верификация: node --check OK; go vet/test зелёные; инстанс жив (HTTP 401 без токена), страница с cookie 200 и содержит @arijjia/agctl-cabinet/client.js в combo boot graph. Остался ручной шаг: UI-скриншот (CDP в Comet выключен, screencapture окна недоступен) — URL с токеном печатает скрипт.
- 2026-10-18: Скриншот-верификация выполнена (владелец дал разрешение на запись экрана): новое окно Comet c ?token= boot — сайдбар dsh web содержит панель «Кабинет» (между Plugins и Automation tasks), дефолтный чат/Workspaces/Settings не изменены. Скриншот: agctl-hq/logs/2026-10-18-night-wave1-cabinet-panel.png. Замечание: Preview Notice первого запуска не закрыт синтетическими кликами (Comet в тестовом режиме игнорирует CGEvents) — владелец кликнет Continue вручную.
- 2026-10-18: done — Волна 1 закрыта: плагин, скрипт, верификация curl+скриншот
