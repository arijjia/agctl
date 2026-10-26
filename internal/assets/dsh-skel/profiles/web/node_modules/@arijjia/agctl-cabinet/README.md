# @arijjia/agctl-cabinet — профиль v3.2

Клиент-плагин dsh web без отдельной панели/режима. Внизу штатного бокового
сайдбара показывает пользователя; по нажатию открывает popup **Профиль / DSH
Настройки / Справка**. Профиль и кабинет — один экран: роль, статистика, заявки
и SSH для developer/admin. Плагин не даёт пользователю управлять сном
контейнера: рантаймом владеет agctl serve, он просыпается по запросу и сам
засыпает по idle TTL.

## Workspaces

Список, иконки папок, фильтры, статусы сессий и кнопка добавления — **родной
DSH Workspaces браузер**. Проекты-каталоги регистрирует host-плагин
`agctl-chat-session-files` при старте; chat-brand расширяет только родную
кнопку «Add workspace» диалогом:

- пустой воркспейс: имя → `POST /api/projects` `{name}`;
- developer/admin: дополнительно публичный GitHub HTTPS URL → `{name, cloneUrl}`.

После API создаётся реальный каталог `/workspace/<name>`, затем client регистрирует
его штатным `workspaces.create({path})`. DSH сам рисует workspace и сессии.
Кабинет не дублирует список воркспейсов отдельной вкладкой.

## Контракты

- `GET /api/me` → `{user:{login,role}}`; роль показывается отдельным бейджем с
  пояснением.
- User: `POST /api/role-requests` с `{motivation}`; admin видит очередь и
  подтверждает/отклоняет через `/api/role-requests/{id}/approve|reject`.
- Developer/admin: `GET/PUT /api/me/ssh` с `{pubKey}`. Пустой ключ очищает
  сохранённый ключ; изменение вступает в силу при следующем старте контейнера.
  Порт доступен только на loopback сервера; для удалённого подключения нужен
  SSH-туннель.
- Profile stats читаются из DSH session store: суммарные provider-reported
  token totals/breakdown по сессиям и годовая heatmap последних обновлений
  сессий. Heatmap — активность, не token-per-day: такого ряда текущий list
  projection не отдаёт.
- `GET /api/me/runtime` — только status/sshPort для профиля. UI-команд
  start/stop/logs нет; idle TTL и health recovery ведёт agctl serve.
- Воркспейсы доступны через нативный DSH sidebar и `/api/projects`; обычный
  Git autosave остаётся локальным. Клон сохраняет `origin`, но автопуш и
  приватные credentials не настраиваются — отдельная opt-in карточка.

## Реализация и проверка

`lib/client.js` — hand-written Lazy-CJS bundle, React/DSH API не форкаются.
Чип профиля регистрируется в **штатном list-слоте `sidebar.footer.action`**
(над нативным Settings, single-слот `sidebar.settings` не затрагивается);
collapsed-состояние приходит ownerProps `wide`. Host-side регистрацию
Workspaces ведёт `agctl-chat-session-files`; composer hint берётся из штатного
DSH locale contract (`locale.addLanguage/register`), не перезаписывается DOM.

Embedded-копия в `internal/assets/dsh-skel/.../node_modules/@arijjia` должна
совпадать побайтно. Проверка: `node --check lib/client.js` и
`node --test test/client.test.cjs`; ручной smoke — на тестовом профиле, не на
живой сессии владельца.