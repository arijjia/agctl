# agctl — текущая архитектура

> Дата: 2026-10-23. Описывает фактическое состояние `main` на коммите
> `6635f04` (двухрежимная платформа; волны W1–W4 чат-полировки включены).
> Что дальше и в каком порядке — [architecture-roadmap.md](architecture-roadmap.md).
> Дословные требования владельца от 2026-10-19 и стратегическая рамка —
> `agctl-hq/docs/agctl-platform-concept-v2.md`; предыдущая версия этого
> файла (с дословными требованиями и статусами на 2026-10-19) — в истории git.
>
> **Этот документ и карта системы описывают одно и то же.** Карта:
> `system/map/overview.html` — в репозитории; на работающем сервере —
> страница `/map` (за сессией agctl, `internal/server/map.go:34`). Разделы
> ниже повторяют узлы и рамки карты; таблица соответствия — §10.

## 1. Система одним абзацем

`agctl serve` — один Go-бинарь, который на одном домене и под одной
cookie-сессией ведёт **два режима**: после входа пользователь сразу попадает
в **личный чат DeepSeek Harness** (режим 1: ленивый per-user контейнер dsh
web, персистентный том, Git не нужен), а разработчик из dsh-плагина
**«Кабинет»** создаёт **git-backed рабочие места** (режим 2: bare-репа +
клон + свой контейнер, опционально за апрувом администратора; локальный
backend — experimental). Состояние — файлы на диске `$AGCTL_HOME` (SQLite +
тома), контейнеры расходные. Внешние зависимости: Docker и
OpenAI-совместимый LLM-провайдер (корп. LiteLLM).

## 2. `agctl serve` — управляющий слой (рамка карты «один бинарь»)

| Часть | Что делает | Код |
|---|---|---|
| REST API | пользователи, чат-папки, воркспейсы, заявки, админка, local-backend, `/map` | `internal/server/server.go:214-293` |
| Auth | argon2id, cookie-сессии, rate-limit логина; первый пользователь = админ | `internal/server/handlers.go:56-117`, `server.go:128-141` |
| Секреты | AES-256-GCM, мастер-ключ `secrets.key` 0600; заявки и провайдеры хранятся шифротекстом | `internal/secrets`, `handlers.go:923` |
| Store | SQLite (WAL) + авто-миграции 001–010 (requests, local_connectors, settings, base_urls) | `internal/store/migrations/` |
| Фоновые задачи | Jira-поллер (1 мин, если настроен), chat-reaper (1 мин), autosave (2 мин), reconcile (30 c), чистка сессий (1 ч) | `server.go:341-392` |
| Платформенный LLM | `--llm-*` сохраняется зашифрованным в settings и восстанавливается после рестарта | `server.go:146,152-183` |

## 3. Один домен — три пути (стрелка «HTTPS / сессия» на карте)

| Путь | Куда ведёт | Код |
|---|---|---|
| `/` | без сессии → `/app/` (SPA входа); с сессией → `/chat/` (флаг `--tier0`, по умолчанию вкл) | `server.go:295-308` |
| `/chat/*` | режим 1: reverse proxy в **личный** dsh-контейнер пользователя + pre-paint чат-шелл + cookie-mint | `internal/server/chat.go:568-675` |
| `/w/{id}/*` | режим 2: reverse proxy в контейнер выбранного dev-места (owner/admin, mint, WS/SSE) | `internal/server/gateway.go:73-162` |
| `/api/*` | управление: регистрация/сессии, папки чата, CRUD мест, заявки/апрув, провайдеры, SSH, local-backend | `server.go:217-272` |
| `/map` | карта системы (росток будущей админ-панели), за сессией | `internal/server/map.go` |

CORS включается только явным `--cors-origin` (allow-list, без wildcard с
credentials) — `server.go:289-292,313-335`.

## 4. Режим 1 «Чат» — ленивый per-user dsh runtime

Рамка карты «Режим 1 · Чат (per-user, lazy)»: узлы *Chat runtime (Docker,
lazy)* и *chat-brand + session-files*.

- **ensure-on-request**: при обращении к `/chat/` под личным мьютексом
  поднимается контейнер пользователя (`ensureChat`, `chat.go:423-475`);
  пока прогрев — HTML-страница авто-ретрая (`chat.go:545-549`). Имя
  рантайма включает hash `$AGCTL_HOME` + user id (`chat.go:55-58`),
  StorageID = `u<id>` (`chat.go:60`).
- **TTL reaper**: контейнер засыпает после 15 минут простоя
  (`--chat-ttl-min`, reaper `chat.go:479-505`); `StopChat` удаляет только
  compute — **том переживает сон и удаление контейнера**.
- **Тома**: `$AGCTL_HOME/chats/u<id>/{dsh-home,work}`
  (`internal/runtime/dockerrt/chat.go:24-25`); сид скелета и плагинов при
  первом буте, `RefreshProvider` на каждом ленивом старте
  (`dockerrt/chat.go:31-49`).
- **Per-session cwd**: контейнеру задаётся
  `AGCTL_CHAT_SESSION_ROOT=/workspace/.sessions` (`docker.go:266-269`);
  host-плагин **chat-session-files** создаёт на каждую DSH-сессию папку
  `.sessions/<sessionId>` и внутренний Workspace с этим cwd
  (`dsh-plugins/chat-session-files/lib/index.js:25-54`); сервер проверяет
  пути symlink-safe (`safeChatSessionDir`, `chat.go:284-340`). Это привязка
  файлов, **не security boundary** между сессиями одного пользователя.
- **Папки сессий и инструкции проекта** (с `6635f04`): состояние папок
  per-user хранится на сервере в settings KV — `GET/PUT /api/chat/folders`
  (`chat.go:78-136`; лимиты 256 КиБ / 64 папки / 5000 назначений);
  инструкции папки управляемым блоком пишутся в `AGENTS.local.md` каждой
  назначенной сессии — `PATCH /api/chat/sessions/{id}/folder-instructions`
  (`chat.go:202-280`); новые сессии привязываются к активной папке. Папка —
  группировка в UI, не каталог-граница.
- **Trajectory**: возвращена включением штатной настройки developerTools dsh
  (chat-brand `client.js:588-596`), без DOM-патчей.
- **Провайдер LLM**: приоритет — личный секрет пользователя → платформенный
  по умолчанию → если пусты оба, dsh показывает свой модал «Add an API key»
  (`userProvider`, `chat.go:403-419`).
- **ChatProvider-контракт** (шов под K8s): `ChatSpec{RuntimeID, StorageID}` /
  `ChatHandle{Endpoint}` / `EnsureChat, StopChat, LogsChat`
  (`internal/runtime/chat.go:15-40`); Docker-реализация —
  `dockerrt/chat.go:17-74`, порт публикуется только на 127.0.0.1
  (`docker.go:272`).

### 4.1 UX-слой чата (почему не «мигает»)

- **Чат-шелл на шлюзе**: pre-paint инъекция CSS/JS в HTML `/chat/` до
  первой отрисовки — университетский бренд, скрытие dev-панелей и пикеров,
  только agctl-квинсы в выборщике моделей; пользовательский контент не
  переписывается, лимит 1 МиБ, при ошибке поток идёт как есть
  (`internal/server/chatshell.go:3-31,127-213,291-314`).
- **chat-brand** (v0.2.6): бренд, русские плейсхолдеры, плоский список
  «Сессии», скрытие workspace-панелей, **папки сессий** (чипсы, контекстное
  меню, настройки папки-проекта, автопривязка, синк с сервером)
  (`dsh-plugins/chat-brand/lib/client.js:122-572,588-596`).
- **Кабинет-навигация**: секция «РЕЖИМЫ» в сайдбаре — «Личный чат» (синяя)
  и дев-воркспейсы (оранжевые, статус-точки), same-origin переходы,
  collapse-aware рельс иконок
  (`dsh-plugins/cabinet/lib/client.js:752-867`).
- Плагины сеются только в **chat**-домашки через `EnsureChatPlugins`
  (`provision.go:320-401`); в dev-местах бренд-плагина нет.

## 5. Режим 2 «Разработка» — git-backed рабочие места

Рамка карты «Режим 2 · Разработка (апрув админа)»: узлы *Dev-workspace
(Docker)* и *Плагин «Кабинет»*.

- **Кабинет** (v0.6.3): создание места (комплект `base`/`1c-vibe` +
  компоненты, **несколько Base URL и несколько моделей**, SSH-ключ), «Мои
  заявки», админ-очередь апрува, логи контейнера, настройки существующего
  места (`dsh-plugins/cabinet/lib/client.js:171-475,582-728`).
- **Заявки и апрув**: с флагом `--require-approval` создание места идёт
  через `POST /api/workspace-requests` — провайдер шифруется, админ
  апрувит/отклоняет, квота перепроверяется при апруве, затем запускается
  provision (`handlers.go:843-1093`; миграция `007_workspace_requests.sql`).
- **Provision-конвейер** (`internal/provision/provision.go:193-278`):
  bare-репа `repos/<owner>/<имя>.git` («эмуляция GitLab») → рабочий клон
  `work/<id>/repo` + компоненты комплекта + манифест `agctl-kit.json` +
  первый push → `SeedDshHome` (скелет, `workspace.json`, `settings.yaml` с
  алиасами провайдеров `agctl`, `agctl-2`…, `.credentials.yaml` 0600) →
  контейнер. Каталог комплектов — `internal/provision/kits.go:35-58`.
- **Шлюз `/w/{id}/`**: проверка owner/admin на каждом запросе; boot-токен
  dsh в браузер не отдаётся (cookie-mint: upstream-401 → серверный повтор с
  `?token=`, `gateway.go:3-13,193-221`); `dsh-auth-*` cookie скоупится на
  `/w/<id>/` (`gateway.go:130-144`); WS/SSE без буферизации
  (`gateway.go:113`).
- **Смена провайдера**: multi-URL (до 8) и мульти-модели,
  `PATCH /api/workspaces/{id}/provider` → `RefreshProvider` + пересоздание
  контейнера (`server.go:660-725`, `handlers.go:653-757`; миграция `010`).
- **SSH/IDE-петля**: sshd в контейнере только при переданном ключе,
  key-only, порт только на 127.0.0.1 хоста; `agctl connect`, `agctl sync`
  (Mutagen two-way-safe), VS Code Remote-SSH
  (`handlers.go:545-649`; `cmd/agctl/connect.go`, `cmd/agctl/sync.go`).
- **Autosave**: каждые 2 минуты и при стопе — commit+push в bare
  (`provision.go:508-544`, `server.go:356-367`); reconcile помечает
  умершие контейнеры stopped (`server.go:368-379,412-434`).

## 6. Local backend (experimental) — «бэк локально, фронт на сервере»

Рамка карты «Local backend (experimental)»: узлы *agctl local connect* и
*dsh web на ноутбуке*. Схема: браузер → `/api/local-backend/ui/*` на agctl →
outbound WSS (`/api/local-backend/connect`, yamux-стримы) → коннектор на
ноутбуке → локальный `dsh web` на loopback; Host/Origin переписываются на
authority коннектора (`internal/bridge/bridge.go:151-373`).

- Включается флагом `--experimental-local-bridge` (только с
  `--secure-cookies` вне loopback); маршруты при выключенном флаге честно
  отдают 404 (`server.go:245-272`, `local_bridge.go:23-25`).
- Pairing: одноразовый грант (TTL 2 мин) → при первом подключении выдаётся
  durable credential `agctl-conn-1-…`; на сервере хранится только SHA-256
  hash (SQLite `local_connectors`, миграция `008`), raw — в файле 0600 на
  ноутбуке (`local_bridge.go:15-95`, `cmd/agctl/local.go:144-178`).
- `agctl local connect --server … --grant … [--launch-dsh]`: переподключение
  с backoff, 401/403 без ретраев (`cmd/agctl/local.go:28-137`); revoke
  удаляет запись и живую сессию (`local_bridge.go:107-118`).
- **Статус: experimental, готов к пилоту, не production** — до продакшена
  нужны rate-limit на `/connect`, доверенный TLS ingress и ops-ревью.
  Проверка: `scripts/e2e-local-bridge.sh`.

## 7. K8s — prototype (узел карты «K8s ChatProvider»)

`internal/runtime/k8srt`: per-user Pod + ClusterIP Service + PVC/Secret от
StorageID (Pod/Service — от RuntimeID), init-container
`agctl init-chat-home`, non-root + seccomp + без SA-token, ingress
NetworkPolicy (`k8srt/provider.go:127-386`). **Не подключён**: `server.New`
по-прежнему выбирает Docker; не ждётся readiness, нет live-cluster smoke,
egress открыт; K8s-адаптера для dev-workspaces (режим 2) нет
(`server.go:106-108`; ограничения и ёмкость — `docs/capacity.md:100-105`).

## 8. Данные на диске — `$AGCTL_HOME` (по коду)

```
$AGCTL_HOME/                     # ~/.agctl или --home (main.go:99-108)
  agctl.db                       # SQLite WAL; миграции 001-010
  secrets.key                    # мастер-ключ AES-256-GCM (0600)
  chats/u<userID>/dsh-home       # режим 1: DSH_HOME (переживает контейнер)
  chats/u<userID>/work           # режим 1: рабочий том; .sessions/<sessionId>
  repos/<owner>/<name>.git       # режим 2: bare-origin («эмуляция GitLab»)
  work/<id>/repo                 # режим 2: рабочий клон (в контейнере)
  work/<id>/dsh-home             # режим 2: DSH_HOME места
```

В settings-таблице БД лежат платформенный провайдер (`chat_default_llm`) и
папки сессий per-user (`server.go:146`, `chat.go:90-92`). Бэкап платформы —
снимок каталога.

## 9. Безопасность и обновляемость

**Границы доверия (карточка карты «Границы доверия»):** boot-токен dsh ходит
только сервер↔runtime и не попадает в браузер (`gateway.go:5-13`);
`agctl_*` cookie не проходят в контейнеры (`gateway.go:243-255`); `dsh-auth-*`
cookie скоупятся на путь режима (`gateway.go:130-144`, `chat.go:645-659`);
same-origin fence на обоих шлюзах (`gateway.go:78-81,166-184`,
`chat.go:596-599`); credential коннектора — hash на сервере, raw 0600 на
ноутбуке; revoke удаляет запись и живую сессию. Изоляция пользователей —
контейнеры и тома per-user; файловые папки сессий — не граница (§4).

**Обновляемость dsh**: ядро не форкаем — плагины (cabinet, chat-brand,
chat-session-files), pre-paint чат-шелл на шлюзе и поддерживаемые конфиги;
версия пинится в Dockerfile (`@deepseek-ai/dsh@0.2.0-rc.2`,
`internal/assets/image/Dockerfile:24`), обновление — `scripts/dsh-bump.sh` +
e2e. Bump до 0.2.1-rc.2 заблокирован очисткой Docker storage (роадмап §4).

## 10. Соответствие карты и документа

| Узел/рамка карты | Раздел | Код |
|---|---|---|
| Браузер | §3 | — |
| agctl serve (auth · гейты · mint · reaper) | §2 | `internal/server` |
| SPA /app | §3 | `web/` |
| SQLite + secrets.key | §2, §8 | `internal/store`, `internal/secrets` |
| /map — карта системы | §3 | `internal/server/map.go`, `system/embed.go` |
| Chat runtime (Docker, lazy) | §4 | `internal/server/chat.go`, `internal/runtime/dockerrt/chat.go` |
| chat-brand + session-files | §4.1 | `dsh-plugins/chat-brand`, `dsh-plugins/chat-session-files` |
| LiteLLM корп. | §4, §5 | провайдеры: `chat.go:403-419`, `provision.go:460-498` |
| Dev-workspace (Docker) | §5 | `internal/provision`, `internal/runtime/dockerrt` |
| Плагин «Кабинет» | §5 | `dsh-plugins/cabinet` |
| agctl local connect + dsh на ноутбуке | §6 | `internal/bridge`, `cmd/agctl/local.go` |
| K8s ChatProvider | §7 | `internal/runtime/k8srt` |
