# @arijjia/agctl-cabinet — кабинет v3 (0.7.0)

Клиент-плагин dsh web: единый личный runtime `/chat/`, проекты, заявки на
роль developer и персональный SSH. Режимов и переходов между workspace нет.
Legacy workspace API и experimental local backend UI не используются.
Серверные реализации этих возможностей плагин не изменяет.

## Контракты

- Авторизация: `POST /api/login` с `{login,password}`, cookie credentials.
- `GET /api/me` → `{user:{login,role}}`.
- User: мотивация `POST /api/role-requests` с `{motivation}`, список своих
  заявок через `GET /api/role-requests`.
- Admin: очередь заявок developer и решения
  `POST /api/role-requests/{id}/approve|reject`.
- Список заявок: `{role,requests:[{id,userId,login,status,motivation,
  decidedBy,decidedAt,createdAt}]}`. Панель показывает историю решений;
  кнопки решений доступны только admin для pending-заявок.
- Developer/admin: `GET /api/me/ssh`, `PUT /api/me/ssh` с `{pubKey}`;
  ответ `{pubKey,sshPort}`. Пустой ключ отключает SSH. Порт 0 означает сон
  runtime; открыть чат и обновить кабинет. Команда:
  `ssh root@127.0.0.1 -p PORT`. Loopback относится к серверу: для удалённого
  доступа необходим SSH-туннель. Приватные ключи не отправляются.
- Всем ролям: `GET /api/projects` → `{projects:[{name,directory}]}`,
  `POST /api/projects` с `{name}`. `directory` — контейнерный путь
  `/workspace/<name>`, не host path. После создания Кабинет регистрирует его
  через официальный DSH `workspaces.create({path})`, чтобы чат мог запускать
  новые сессии в каталоге проекта.

## Регистрация и оформление

`lib/client.js` — рукописный Lazy-CJS bundle через
`window.__ModuleLoader__.load`, сборка не нужна. Сохранены стили/бренд agctl,
`sidebar.panellist` («Кабинет», id `agctl-cabinet`) и keyed slot `main`.
`lib/index.js` — host no-op; `cordis.patch.yml` регистрирует пакет.
`package.json` сохраняет dsh.client platform web и layout injection.

Пустое поле URL использует текущий origin. Явный URL хранится в localStorage;
для кросс-доменного подключения сервер должен разрешать соответствующий
origin и credentials. Ссылка в кабинете ведёт на `/chat/` выбранного сервера.
В боковой навигации только личный чат, без workspace-запросов и таймеров.

Копия пакета в embedded web profile должна быть побайтно идентична исходной.
Проверка: `node --check lib/client.js`, `node --check lib/index.js`,
`node --test test/client.test.cjs` (mock-проверки ролей, payload, SSH-сна,
истёкшей авторизации и отсутствия legacy-навигации).
Для ручного smoke используйте изолированный профиль/TEST_HOME, не живой
профиль владельца и не порт 3080. Не сохраняйте токены в репозитории.
