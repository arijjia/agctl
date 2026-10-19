# @arijjia/agctl-cabinet — панель «Кабинет» для dsh web

Клиент-плагин DeepSeek Harness: добавляет боковую панель dsh web «Кабинет»
для управления рабочими местами agctl и опционального SSH-доступа.

- Дефолтный чат dsh web не меняется — Tier 0 остаётся базовым интерфейсом;
  кабинет появляется как отдельная панель.
- Кабинет работает через agctl REST: вход, список/создание/старт/стоп мест,
  подключение SSH public key при создании и показ SSH-порта.

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

## Волна 2: живой agctl REST

Экран кабинета работает против настоящего `agctl serve`: вход
(`POST /api/login`), `GET /api/me`, список мест (`GET /api/workspaces`),
создание (`POST /api/workspaces`), старт/стоп (`POST .../start|stop`). При
создании можно вставить `ssh-ed25519 ...` публичный ключ: он передаётся как
`sshPubKey`, в карточке места появится `sshPort` и команда `agctl connect`.
Ключ можно заменить/снять через `PATCH /api/workspaces/{id}/ssh`; приватная
часть ключа никогда не покидает машину пользователя.

- База адреса: если поле URL пусто — запросы идут относительно текущего
  origin (случай «dsh открыт через шлюз agctl `/w/<id>/`»: same-origin,
  cookie agctl уже стоит). Иначе — указанный URL.
- Кросс-доменный случай (панель в прямом dsh, agctl на другом порту)
  требует разрешить origin на agctl:

  ```sh
  agctl serve --cors-origin "http://127.0.0.1:3555"
  ```

  Список пуст по умолчанию — CORS выключен; wildcard с credentials не
  используется никогда (`internal/server/cors_test.go`).

## Проверка волны 2 (ночь 2026-10-18→19)

```sh
# терминал 1: изолированный dsh web с панелью
scripts/night-profile.sh
# терминал 2: agctl с CORS на тестовом home
/tmp/agctl-night serve --home /tmp/agctl-night-agctl-home \
  --addr 127.0.0.1:18929 --cors-origin "http://127.0.0.1:3555"
```

Открыть `http://127.0.0.1:3555/` (скрипт печатает URL с boot-токеном) →
Кабинет → URL `http://127.0.0.1:18929` → вход тестовым пользователем →
список мест/создание. Живой провод подтверждён: combo-бандл dsh содержит
код панели (проверено curl по boot graph), agctl отвечает с ACAO.

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
