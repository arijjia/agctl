# agctl v3 — текущая архитектура

> Срез `main`: объединённый персональный runtime, роль `developer`, проекты
> в персональном томе. Решения владельца: TTL 1 час; developer получает SSH
> после заявки и апрува, роль закрепляется за учёткой; пилот — на чистом томе;
> Git для обычного пользователя невидим; локальный backend отложен.
>
> **Карта и документ — два слоя:** карта физическая (процессы, адреса и
> протоколы) — `system/map/agctl-processes.html`, в браузере `make map`, на
> сервере `/map`; этот документ объясняет продуктовые сущности и их связь с
> кодом. Подробное обучение — `agctl-hq/docs/v3/architecture-explained.md`,
> сравнение вариантов и решений — `agctl-hq/docs/v3/unified-runtime-model.md`.

## 1. Модель v3 одним абзацем

`agctl serve` — один Go-процесс, единый домен и точка входа. **Каждый
пользователь** получает ровно один персональный dsh-контейнер: он лениво
поднимается при открытии `/chat/`, засыпает после 60 минут простоя, а том с
DSH_HOME, сессиями и проектами остаётся на диске. Папка в боковом дереве —
реальный каталог проекта `/workspace/<name>` и настоящий DSH Workspace.
Создание проекта делает локальный Git-репозиторий; go-git выполняет
автосохранение каждые две минуты внутри процесса agctl, без Git-процесса,
remote и push. Обычный пользователь Git не видит. Роль `developer` добавляет
sshd в тот же контейнер — отдельный dev-контейнер не создаётся.

## 2. Роли и возможности

| Роль | Как появляется | Runtime/capabilities |
|---|---|---|
| `user` | регистрация | персональный чат, проекты-папки, invisible local git-autosave |
| `developer` | `POST /api/role-requests` → решение admin → `users.role=developer` | всё user + SSH key-only в том же контейнере; SSH-порт loopback |
| `admin` | первый аккаунт или назначение | админ-панель, пользователи/аудит, очередь заявок developer; технически имеет developer-capabilities |

Код заявки/апрува: `internal/server/roles.go`, store-методы —
`internal/store/roles.go`, схема — `internal/store/migrations/011_role_requests.sql`.
Роль постоянна и читается из записи пользователя при каждом запросе — это
не cookie-роль, не отдельный контейнер и не переключаемый режим.

SSH-ключ задаётся через `PUT /api/me/ssh`, хранится в SQLite settings по
ключу `dev-ssh-pubkey-u<ID>`. При следующем старте `ensureChat` передаёт
его в `ChatSpec.SSHPubKey`; `dockerrt.EnsureChat` просит Docker опубликовать
`:2222` только на `127.0.0.1` и возвращает `SSHPort`. Entry-point запускает
sshd только при непустом ключе (`internal/assets/image/entrypoint.sh:22-45`).
GET `/api/me/ssh` возвращает `sshPort=0`, если персональный контейнер спит.

## 3. `agctl serve` и жизненный цикл runtime

### 3.1 Управляющий процесс

`cmd/agctl/main.go` → `cmdServe` загружает конфигурацию, мастер-ключ и
SQLite, создаёт `server.Server`, запускает фоновые горутины и слушает адрес
(по умолчанию `127.0.0.1:8929`). Это **один процесс**; reaper, autosave,
reconcile и очистка сессий — горутины внутри него, не дочерние процессы
(`internal/server/server.go:352-402`).

Основные части `Server` (`internal/server/server.go`):

| Ответственность | Реализация |
|---|---|
| HTTP routes + middleware | `Routes` (`server.go:214-315`) |
| Auth, cookie sessions, roles | `internal/auth`, `handlers.go`, `roles.go` |
| Персональные chat-runtimes: ensure/locks/TTL/eviction | `internal/server/chat.go` |
| Проекты + локальный autosave | `internal/server/projects.go`, `internal/provision/projects.go` |
| Старый provisioner workspaces | `internal/provision/provision.go` (вызывающий HTTP/CLI create выключен) |
| Docker runtime + image health | `internal/runtime/dockerrt` |
| SQLite / secret encryption | `internal/store`, `internal/secrets` |

Логическая изоляция: все пользовательские API проходят `requireUser`,
админские — `requireAdmin`; SSH pubkey/project path выбираются по ID
аутентифицированного аккаунта, а не по произвольному `userID` из JSON.
OS-граница между аккаунтами — их отдельные контейнеры и тома. Каталоги
проектов внутри одного аккаунта — **не** граница безопасности.

### 3.2 Запуск и сон

Container storage uses two RW bind mounts:

- `$AGCTL_HOME/chats/u<ID>/dsh-home` → `/home/dsh` (`DSH_HOME`);
- `$AGCTL_HOME/chats/u<ID>/work` → `/workspace`.

Image sets `DSH_HOME=/home/dsh`; Docker also sets `HOME=/workspace` for tool
processes and redirects npm/XDG caches to `/tmp`. Thus DSH state/settings go to
`/home/dsh`, project files and any tools that write under `$HOME` go to
`/workspace`, while configured caches are ephemeral. DSH Web and its child
processes read/write the RW mounts directly; file I/O is not proxied through
`agctl serve`. The Go server separately provisions project folders and runs
local go-git autosave against the host path. This is the
**current source layout**. `$AGCTL_HOME/u<ID>/{dsh-home,workspace}` is a possible
target layout, not a completed migration; the pilot starts on a clean volume.

1. Пользователь открывает `/`. Без сессии получает `/app/` — React SPA agctl
   из `go:embed`; при входе корень ведёт в `/chat/` (`server.go`, функция
   `registerTier0Routes`).
2. Сам логин контейнер не создаёт. Первый `/chat/` вызывает `ensureChat`
   (`internal/server/chat.go`, сейчас около строк 501–555): per-user lock,
   стабильный `StorageID=u<ID>`, `ChatProvider.EnsureChat`.
3. Пока dsh web стартует, agctl отдаёт собственную HTML-страницу прогрева
   `writeWarmChatPage`; после готовности `/chat/*` проксируется в контейнер.
4. Reaper проверяет runtime раз в минуту. При `active==0` и idle ≥ TTL
   (по умолчанию 60 мин; `--chat-ttl-min`) удаляется контейнер compute;
   DSH_HOME/work volume не удаляются (`chat.go`, `reapIdleChats`).
5. Docker HEALTHCHECK проверяет HTTP на `127.0.0.1:3099`. Reconcile раз в
   30 секунд видит статус `unhealthy`: legacy workspace помечает stopped,
   unhealthy chat-runtime останавливает и выкидывает из кэша; следующий
   `/chat/` пересоздаёт контейнер на прежнем томе (`Dockerfile`,
   `dockerrt.HealthStatus`, `server.go` `reconcile`).

**Ограничение:** active SSH-сессия продлевает `lastSeen`: в последнюю минуту
до TTL Docker `ContainerTop` проверяет sshd child в процессе контейнера.
Если проверка Docker временно падает, reaper пропускает этот тик (fail-closed),
чтобы не оборвать сессию вслепую. Detached-процесс после закрытия SSH не
обнаруживается; его учёт — отдельная задача.

## 4. Проекты-папки, DSH Workspace и Git

### 4.1 Где проект живёт

Host-путь проекта:

```
$AGCTL_HOME/chats/u<ID>/work/<project-name>/
  .git/                    локальная история; у созданного с нуля нет remote
  .gitignore               .env*, .credentials*, .ssh исключены
  README.md
  AGENTS.local.md          управляемые инструкции проекта
  ...                      файлы пользователя/агента
```

В контейнере этот же каталог виден как `/workspace/<project-name>`. API
`GET/POST /api/projects` возвращает только имя и контейнерный путь, не
раскрывая host path; имя валидируется как один компонент пути, симлинки
запрещены (`internal/provision/projects.go`, `internal/server/projects.go`).
Developer/admin при создании может передать `cloneUrl` (только http/https):
репозиторий клонируется в воркспейс без креденшелов; remote остаётся
настроенным клоном, но автопуш не выполняется.

### 4.2 Folder → настоящий cwd

1. Создание папки из дерева chat-brand вызывает `POST /api/projects` —
   создаются каталог и локальный Git.
2. Клиент DSH регистрирует Workspace по `path` через официальный
   `workspaces.create({path})`; этот метод создаёт/переиспользует запись в
   workspace registry dsh.
3. «＋» на папке вызывает `uiWorkspace.startSession(workspaceId)`.
4. Host-плагин `agctl-chat-session-files` видит явно заданный project
   `workspaceId` и **не** заменяет его скрытым cwd `.sessions/<sessionId>`.
   Поэтому новый Session реально запускается в `/workspace/<project>`.

Сессии, созданные без проекта, сохраняют старое поведение — отдельный
`.sessions/<sessionId>` и внутренний Workspace «Chat». Переназначение старой
сессии в другую папку меняет UI-группировку, но не переносит её файлы между
каталогами; пилот рассчитан на чистый том, миграции нет.

Project instructions записываются в `AGENTS.local.md` корня проекта через
`PUT /api/projects/{name}/instructions` (и совместимый PATCH endpoint чата).
Скрытие папки из дерева не удаляет каталог, Git-историю и инструкции.

Личным рантаймом управляет developer из Кабинета: `GET /api/me/runtime`
(статус/sshPort), `POST /api/me/runtime/start|stop`, хвост логов
`GET /api/me/runtime/logs?tail=N`. Стоп — то же, что делает TTL-reaper:
удаляет compute, том и сессии остаются; старт — ленивый `ensureChat`.

### 4.3 Невидимое Git-версионирование

`CreateProject` и `AutosaveLocalProject` реализованы на go-git
(`internal/provision/projects.go`): проект с нуля получает локальный `.git`
без remote; `CloneProject` сохраняет `origin` публичного GitHub-клона. Autosave
раз в две минуты делает commit локально и **никогда не push-ит**. Отдельного
Git-процесса нет. Для `user` Git не показывается; developer может работать
с `origin` вручную из SSH/терминала, но credentials и автопуш не настраиваются.
`.env*`, `.credentials*`, `.ssh` не попадают в autosave-историю.

## 5. Порты и сетевой путь

| Порт | Кто слушает | Где доступен |
|---|---|---|
| `127.0.0.1:8929` | `agctl serve` | только loopback хоста |
| `127.0.0.1:<dynamic>` | опубликованный Docker `:3080` relay | loopback хоста; agctl reverse-proxy |
| `0.0.0.0:3080` | `agctl-relay` внутри контейнера | интерфейсы контейнера; dsh наружу через него |
| `127.0.0.1:3099` | `dsh web` внутри контейнера | только loopback контейнера |
| `127.0.0.1:<dynamic>` → `:2222` | Docker → sshd developer | только loopback хоста |
| исходящий `:443` | dsh/connector → LiteLLM или локальный backend tunnel | исходящие соединения |

Путь чата: browser → `:8929` → auth/gateway `/chat/` → опубликованный
loopback host port → relay `:3080` → raw TCP `127.0.0.1:3099` → dsh web →
LiteLLM `:443`. Публикация Docker выбирает случайный свободный host port;
это не «слушать всю LAN». Подробности релея — в
`agctl-hq/docs/v3/architecture-explained.md` §5–7.

## 6. Legacy compatibility и отложенное

- Создание/изменение отдельных workspace seats и workspace request API
  отвечает HTTP 410; CLI `agctl workspace create` отключена.
- GET старых workspace-записей и `/w/{id}` proxy пока оставлены для
  совместимости с записями старого тома/низкоуровневых тестов; новых seats
  на чистом пилоте нет. Полное удаление кода/таблиц — cleanup backlog.
- `/api/kits` и `internal/provision/kits.go` пока legacy-каталог: проекты v3
  создаются с базовым README, комплекты `base`/`1c-vibe` ещё не подключены.
- Local backend — experimental code behind flag; пользовательский flow
  отложен владельцем.
- Kubernetes `ChatProvider` создаёт Pod/Service/PVC в prototype
  (`internal/runtime/k8srt`), но `cmd serve` выбирает Docker; readiness/live
  cluster smoke, selector, egress policy, PVC backup/encryption и HA ещё не
  реализованы. Production K8s пока не заявляется.
- MFA/SSO и egress allowlist не реализованы; текущие остаточные риски —
  `docs/secure-gateway.md`.

## 7. Физическая карта и код

Физическая карта — `system/map/agctl-processes.html`; источник —
`system/map/agctl-processes.architecture.json` (улики проверяются `archify`
против revision в meta). Узлы — процессы или файлы/внешние сущности, рамки —
реальные host/container границы. Один персональный контейнер v3 содержит
relay + dsh web и, для developer, sshd; `agctl serve` и go-git autosave —
один Go-процесс, горутины не отдельные процессы.
