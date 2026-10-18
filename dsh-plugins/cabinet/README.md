# @arijjia/agctl-cabinet — панель «Кабинет» для dsh web

Клиент-плагин DeepSeek Harness (волна 1 эпика `night-dsh-plugins`, ночь
2026-10-18→19): добавляет в боковую панель dsh web кнопку «Кабинет» и
экран управления рабочими местами agctl.

- Дефолтный чат dsh web не меняется — платформа для пользователя выглядит
  как обычный чат с инструментами (Tier 0), кабинет появляется рядом.
- Волна 1: экран-каркас — проверка подключения к agctl (`GET /api/me`)
  и список-заглушка. Волна 2: живой agctl REST (список/создание мест).

## Как это устроено

- `lib/index.js` — host-половина, no-op (пакет виден как Loader-строка;
  паттерн `dsh-client-ui-jobs`).
- `lib/client.js` — браузерная половина: рукописный Lazy-CJS-бандл в
  формате хоста (`window.__ModuleLoader__.load({id, factory})`), поэтому
  шаг сборки не нужен. Регистрация: `sidebar.panellist` (id
  `agctl-cabinet`, label «Кабинет») + keyed slot `main` с тем же ключом —
  паттерн `dsh-client-ui-plugin-manager`.
- `package.json → dsh.client`: `platform: "web"`,
  `inject: ["@deepseek-ai/dsh-client-ui-layout"]` (даёт сервис `layout`;
  `slots` — базовый сервис ядра).

## Запуск (изолированно)

```sh
scripts/night-profile.sh        # поднять dsh web на http://127.0.0.1:3555
scripts/night-profile.sh stop   # остановить
```

Скрипт держит собственный `DSH_HOME` (`/tmp/agctl-night-dsh-home`),
ставит пакет в профиль `night-cabinet` (`dsh plugin --profile … add
file:…`), включает его в `dsh.profile.bundles` и печатает URL с
boot-токеном для автовхода. Живая сессия владельца (:3080) не трогается.

## Не делать

- Не поднимать этот профиль на порту 3080 и в дефолтном `$DSH_HOME`.
- Не коммитить токены из логов (лог живёт в /tmp, вне репо).
