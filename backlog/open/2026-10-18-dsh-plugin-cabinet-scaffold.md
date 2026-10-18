---
schema: 1.0.0
id: dsh-plugin-cabinet-scaffold
title: "Каркас клиент-плагина «Кабинет agctl» в dsh web (панель sidebar.panellist)"
status: in-progress
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
