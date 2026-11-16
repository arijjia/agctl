# Локальный режим agctl: архитектура, реализация и разбор кодовой базы

> Срез `main` на 2026-11-16. Этот документ — единственная актуальная
> документация в репозитории: README — онбординг разработчика, здесь —
> как локальный режим устроен и реализован, какая часть кодовой базы
> отвечает за локальный режим, какая за удалённый (серверный) и кластерный,
> и как читать репозиторий целиком. Прежние документы (серверная
> архитектура v3, Kubernetes, кастомизация профилем, безопасность шлюза,
> capacity, сборка и CLI) лежат архивом в `agctl-hq/docs/`. Физические
> карты — `make map` (три вкладки: сервер, ноутбук, кластер), улики на
> картах ведут в строки кода.

## 1. Одно ядро, три бинаря, три режима

Платформа — одно ядро (`internal/*`) и три статических Go-бинаря поверх
него (карточка `split-binaries-local-server`):

| Бинарь | Кому | Команды | Чего в нём нет |
|---|---|---|---|
| `agctl` (`cmd/agctl`) | разработчику | `dev …`, `backlog`, `completion`, `version` | серверных модулей, k8s/AX, gRPC |
| `agctl-server` (`cmd/agctl-server`) | организации | `serve` (`--runtime docker \| k8s \| ax`), `migrate`, `seed-demo-users`, `connect`, `sync`, `config` | локального режима |
| `agctl-guest` (`cmd/agctl-guest`) | образу `agctl-dsh` (там он `/usr/local/bin/agctl`) | `bridge`, `ax-runner`, `init-chat-home` | сервера |

Общий `serve` — `internal/cli.Serve`: `agctl dev serve` вызывает его с
`LocalMode`, `agctl-server serve` — с `server.ServerModules()` и выбором
бэкенда (`cli.Runtime`, `cmd/agctl-server/runtime_select.go`). Все режимы
используют один и тот же HTTP-сервер `internal/server` и один и тот же образ
рабочего места `agctl-dsh`:

| | Локальный режим `agctl dev` | Серверный режим `agctl-server serve` | Кластер `agctl-server serve --runtime k8s \| ax` |
|---|---|---|---|
| Для кого | один разработчик на своей машине | организация: много пользователей | организация без Docker-хоста |
| Вход | одноразовая ссылка из CLI, регистрации нет | логин/пароль, роли `user` / `developer` / `admin` | как на сервере |
| Рантайм агента | один контейнер на локальном Docker | контейнер на пользователя (dockerd на хосте шлюза) | под (k8s) или актор Agent Substrate (ax) на пользователя |
| `/workspace` | родная папка проектов разработчика (bind mount) | `$AGCTL_HOME/chats/u<ID>/work` | RWX-том / PVC / том актора |
| Дом данных | `~/.agctl-dev` | `~/.agctl` | том шлюза |
| Адрес | `127.0.0.1:8930`, только loopback | `127.0.0.1:8929`, впереди ingress с TLS | Service / Ingress |
| Секреты | хранилище ОС (Keychain, Credential Manager, Secret Service) | `.env` хоста + SQLite (AES-256-GCM) | k8s Secret / egress-инъекция |
| Автосейв проектов go-git | **выключен** (настоящие репозитории) | каждые 2 минуты, локальный commit без push | как на сервере |
| Мост к хосту | есть (белый список команд) | нет | нет |
| Сон по TTL | 12 часов | 60 минут (prewarm-пул, квоты, админка) | удаление пода / `SuspendTask` |

Технически `agctl dev up` ≈ `agctl-server serve` с заполненным `Config.Local`
(`internal/server/server.go`, поле `Local *LocalMode`). Всё серверное, что
локально не нужно (регистрация, роли и SSH developer'а, лаунчер developer'а,
админка, prewarm, autosave, legacy workspaces `/w`, Jira, конфиг раннера
AX), — модули `server.Module` (`internal/server/modules.go`). Их подключает
только серверная точка входа (`Config.Modules = server.ServerModules()`);
локальный режим модулей не подключает, и их маршрутов в нём нет. Немногие
оставшиеся различия внутри ядра — ветки `s.isLocal()` (§2.8).

Решение «контейнер + мост к хосту как основной движок локального режима,
нативный dsh — запасной» зафиксировано карточкой
`backlog/open/2026-11-13-local-mode-v2-container-bridge.md`; концепция и
вопросы к организации — `agctl-hq/docs/local-mode-concept-2026-11-13.md`
и `agctl-hq/docs/local-mode-questions-2026-11-13.md`.

## 2. Как устроен локальный режим

### 2.1 Процессы, порты, файлы

```
Машина разработчика
│
├─ agctl dev <cmd>                 короткоживущий CLI (init, up, status, secrets, bridge …)
│      │  читает ~/.agctl-dev/dev.yaml и run/state.json
│      │  управляет фоновым процессом по HTTP + Bearer ControlToken
│      ▼
├─ agctl dev serve  (фон, 1 Go-процесс, 127.0.0.1:8930)
│      ├─ internal/server с Config.Local: учётка owner (developer), /local/login, /launcher
│      ├─ SQLite + dsh-home владельца:  ~/.agctl-dev/data/{agctl.db, chats/u1/dsh-home}
│      ├─ мост к хосту: белый список argv, подтверждения, журнал logs/bridge.log
│      ├─ Linux: второй слушатель на IP docker0, только пути моста
│      └─ Docker API (internal/runtime/dockerrt — тот же код, что на сервере)
│             │
│             ▼
├─ контейнер рабочего места (agctl-dsh[-web|-<профиль>[-<вкус>]])
│      ├─ dsh web на eth0:3080, опубликован на 127.0.0.1:<случайный порт>
│      ├─ /workspace  ◀── bind mount ── папка проектов (dev.yaml: workdir или аргумент up)
│      ├─ /home/dsh   ◀── bind mount ── ~/.agctl-dev/data/chats/u1/dsh-home
│      ├─ agctl bridge … ──HTTP + Bearer BridgeToken──▶ http://host.docker.internal:8930/api/local/bridge/*
│      └─ ──HTTPS──▶ модель (LiteLLM / публичный провайдер), GitLab, Jira
│
└─ Docker Desktop / Engine / Colima
```

Процессов на машине всегда два: фоновый `agctl dev serve` и Docker. После
«Запустить» в лаунчере (или сразу при входе, если образ и ключ уже есть)
добавляется контейнер с `dsh web`. Мост к хосту процессов не держит: вызов
агента превращается в `exec` одной команды из белого списка.

Дом режима `~/.agctl-dev` (`AGCTL_DEV_HOME` переопределяет,
`internal/localmode.Home`):

| Путь | Что там | Кто пишет |
|---|---|---|
| `dev.yaml` | профиль / `profile_dir`, `image`, `flavor`, `workdir`, `addr`, `mem_gb`, `cpus`, несекретные `env` (адреса, логины), **имена** секретов, `bridge.tools`, `previous_image` | `dev init`, `dev secrets`, лаунчер, `dev update` |
| `data/` | дом сервера: `agctl.db` (SQLite WAL), `secrets.key`, `chats/u1/dsh-home` (сессии и настройки dsh) | `agctl dev serve` |
| `run/state.json` (0600) | PID, адрес, `controlToken`, `loginToken`, образ, папка, время старта, версия и сборка (`build`: версия + коммит) | `dev serve` при готовности; стирается при выходе |
| `logs/serve.log` | stdout/stderr фонового процесса | `dev up` открывает, `dev serve` пишет |
| `logs/bridge.log` | журнал вызовов моста (JSON lines, без вывода команд) | `internal/localbridge` |
| `backups/` | три последних tar-бэкапа dsh-home, делаются перед `update` | `localmode.Backup` |
| `secrets.enc` + ключ | запасное хранилище секретов (AES-GCM), если хранилища ОС нет или `AGCTL_DEV_SECRETS=file` | `localmode.Store` |
| `serve-config.yaml` | заглушка: сюда указывает `AGCTL_CONFIG`, чтобы серверный `~/.config/agctl/config.yaml` не влиял на локальный режим | — |

Серверный дом `~/.agctl` и серверный конфиг CLI на локальный режим не
влияют: `devServe` перед стартом снимает `AGCTL_PROFILE`, `AGCTL_PROFILE_DIR`,
`AGCTL_ADDR`, `AGCTL_DEV_ENV`, `AGCTL_DEV_IMAGES`, `AGCTL_HOME`.

### 2.2 Три токена

`devServe` генерирует три случайных токена (`localmode.NewToken`) и отдаёт
их серверу в `server.LocalMode`:

| Токен | Кто знает | Где проверяется | Зачем |
|---|---|---|---|
| `LoginToken` | `run/state.json` → `dev up` / `dev open` → браузер | `GET /local/login?token=` | одноразовый вход владельца без пароля; живёт до рестарта процесса |
| `ControlToken` | `run/state.json` → CLI на хосте | `requireControl`: `GET /api/local/status`, `POST /api/local/shutdown`, `GET /api/local/bridge/pending`, `POST /api/local/bridge/pending/{id}/{approve\|deny}` | `dev status / down / bridge pending / approve / deny` |
| `BridgeToken` | env контейнера `AGCTL_BRIDGE_TOKEN` и файл `/home/dsh/.agctl/bridge.json` | `requireBridge`: `GET /api/local/bridge/tools`, `POST /api/local/bridge/call` | вызовы моста из контейнера |

Сравнение — `subtle.ConstantTimeCompare` (`tokenEqual` в
`internal/server/local.go`). Файл состояния имеет права 0600; читать его
может только пользователь машины, то есть владелец.

Ручки лаунчера (`/api/local/ui*`) токенов не используют: они за обычной
cookie-сессией владельца плюс проверка same-origin на любой не-GET запрос
(`requireLocalOwner` в `internal/server/local_ui.go`).

### 2.3 Жизненный цикл: от `init` до чата

**`agctl dev init`** (`devInit` в `cmd/agctl/dev.go`):

1. профиль: `--profile ID` (встроенный `agctl`, `profiles/<id>` из исходников,
   `~/.agctl-dev/profiles/<id>`, `~/.agctl/profiles/<id>` или каталог данных
   пользователя `DataDir()` — куда кладёт профиль установщик комплекта;
   если там ровно один профиль организации, он становится дефолтом мастера)
   либо `--profile-dir DIR` (сохраняется абсолютным, чтобы `up` работал из
   любой папки);
2. белый список моста: если в `dev.yaml` его нет, берётся
   `local/bridge-tools.yaml` профиля (`profileBridgeTools`);
3. образ: `--image`, иначе `--flavor` → первый образ профиля с `-<вкус>` в
   имени, иначе первый из `images.dev` профиля, иначе `agctl-dsh:0.2.2`
   (`devDefaultImage`);
4. папка проектов: `--workdir` или вопрос; `$HOME` целиком и `/` запрещены
   (`localmode.ValidateWorkDir`);
5. с `--cli` — мастер в терминале: номер образа, доступы по манифесту
   (`localmode.Manifest`: ключ модели, GitLab, Jira), `docker pull`. Без
   `--cli` образ и доступы настраиваются в лаунчере после `up`.

**`agctl dev up [DIR]`** (`devUp`):

1. `dev.yaml` читается, папка проверяется; если процесс уже жив, но другой
   сборки (поле `build` в `run/state.json` — версия и коммит — не совпадает с
   этим бинарём: например, после `install.sh`), — `dev down` и запуск заново
   (`devStale`), иначе фиксы новой версии не применились бы; живой процесс той
   же сборки с той же папкой — просто открыть браузер, с другой — отказ
   «сначала `dev down`»;
2. `dockerrt.PingDaemon`, свободен ли `addr`;
3. запуск самого себя как `agctl dev serve --workdir DIR` в фоне: stdout в
   `logs/serve.log`, процесс отвязан от терминала (`detach_unix.go`:
   `Setsid`; `detach_windows.go`: `DETACHED_PROCESS` + новая группа);
   `--foreground` — в текущем терминале;
4. ожидание `run/state.json` и ответа `/api/local/status` до 3 минут
   (`devWaitReadyOrExit`), иначе хвост лога;
5. открыть `http://127.0.0.1:8930/local/login?token=…` в браузере
   (`openBrowser`: `open` / `xdg-open` / `rundll32`).

**`agctl dev serve`** (`devServe`, внутренняя команда):

1. `dev.yaml`, хранилище секретов (`localmode.OpenStore`), env контейнера
   (`localmode.ContainerEnv`), ключ модели из хранилища → `AGCTL_LLM_API_KEY`
   процесса (в контейнер он не попадает, его получает сервер как провайдер);
2. `server.LocalMode{OwnerLogin: "owner", WorkDir, LoginToken, ControlToken,
   BridgeToken, BridgeURL: "http://host.docker.internal:<port>", BridgeTools,
   BridgeLog, Env, Hooks: devHooks}`;
3. вызов общего `cli.Serve(serveArgs, cli.ServeOptions{Local, OnReady})` с флагами
   `--home ~/.agctl-dev/data --addr … --mem-gb --cpus --chat-ttl-min 720
   --dev-env= [--profile | --profile-dir] [--image X --dev-images X]`;
4. `onReady`: на Linux — слушатель моста на IP `docker0`
   (`devBridgeListener`, отдаёт только `/api/local/bridge/*`, кроме
   `pending`); запись `run/state.json`; удаление файла при завершении.

**Сервер при старте** (`server.New` → `initLocal` в `internal/server/local.go`):

- учётка `owner` с ролью `developer` создаётся (пароль — случайные 24 байта,
  никому не известен) или получает роль developer, если была;
- `localbridge.New(tools, journal)`;
- `provision.SetLocalWorkRoot(home, ownerID, WorkDir)` — work-корень
  владельца подменяется на его папку (`internal/provision/local_workroot.go`);
- текущий образ из `dev.yaml` записывается в настройку лаунчера
  (`chatImageSettingKey`), чтобы `ensureChat` взял его.

**Вход** (`handleLocalLogin`): проверка `LoginToken`, выдача cookie-сессии
владельцу. Если задан ключ модели и выбранный образ есть на машине —
рабочее место прогревается сразу (`startLocalRuntime(false)`); в любом
случае редирект на `/launcher`.

**Лаунчер** (`web/src/LocalLauncher.tsx`; `App.tsx` показывает его, если
`GET /api/local/ui` отвечает 200, иначе серверный `Launcher.tsx`):

- вкладка «Рабочее место»: каталог образов с отметкой «есть на машине»
  (`devHooks.Images`: `images.dev` профиля без вариантов `nossh` плюс
  текущий), кнопка «Запустить» → `POST /api/local/ui/start`, «Перезапустить»
  → `POST /api/local/ui/restart`, прогресс подготовки (`task.snapshot`:
  состояние `idle | building | starting | error`, последние 40 строк лога);
- вкладка «Доступы»: `Entries` по манифесту и всему, что задано; значения
  никогда не возвращаются, только «задан / не задан» и где хранится;
  `PUT /api/local/ui/secrets/{name}` (`plain: true` — в `dev.yaml`),
  `DELETE …`. Смена ключа модели применяется без рестарта
  (`afterLocalSecretChange` → `setDefaultLLMKey`); смена остальных доступов
  или образа при работающем месте помечает `task.pending` — в UI
  появляется «Перезапустить».

**Старт рабочего места** (`startLocalRuntime` в `local_ui.go`): в фоне —
при рестарте `StopChat` + `evictChatByUser`; если образа нет —
`Hooks.EnsureImage` (pull, иначе сборка цепочки, лог в `task`); затем
`ensureChat(ownerID)` — общий серверный путь.

**`ensureChat`** (`internal/server/chat.go`) в локальном режиме отличается
только дополнениями из `localChatExtras`: `spec.WorkDir` = папка проектов,
`spec.DevEnv` += env доступов + `AGCTL_BRIDGE_URL`/`AGCTL_BRIDGE_TOKEN`;
дальше `writeAgentCredentials` (файлы доступов в dsh-home, §2.5) и
`chatProvider.EnsureChat` → `dockerrt.EnsureChat`:

- `spec.WorkDir` монтируется как `/workspace` вместо `chats/u1/work`;
- dsh-home засевается скелетом при первом старте (`provision.SeedDshHome`),
  провайдер перезаписывается на каждом старте (`RefreshProvider`), плагины
  бренда и дефолт пресета разрешений обновляются;
- контейнер стартует с `HOME=/workspace`, `DSH_HOME=/home/dsh`, портом
  `3080`, опубликованным на `127.0.0.1:<свободный>`; sshd не поднимается
  (ключа нет);
- после старта `writeLocalBridgeSkill` кладёт скилл `agctl-host-bridge`
  в `dsh-home/skills` (dsh подхватывает без рестарта).

**Чат**: браузер → `/chat/` → `chatProxy` (reverse proxy с подменой
boot-токена dsh, инъекцией бренда в `<head>`, страницей прогрева) →
`127.0.0.1:<порт>` → dsh web в контейнере. Тот же путь, что на сервере.

**`agctl dev down`**: `POST /api/local/shutdown` (ControlToken) → graceful
shutdown сервера → контейнер останавливается (`StopChat`), том и dsh-home
остаются.

### 2.4 Обновление и откат

`agctl dev update [--image REF]` (`devUpdate`), пять шагов, с
автооткатом:

1. текущий образ `current` (из `dev.yaml` или по профилю) и цель `target`
   (флаг или тот же тег заново);
2. `docker pull target`; если реестр не отдал, но образ есть локально
   (собран `make img_build`) — берётся локальный; если нет нигде — ошибка,
   ничего не изменено. Если ID образа не изменился — «уже актуально»;
3. если платформа работала — `dev down`; `localmode.Backup(home,
   "before-<target>")` → `backups/dsh-home-<время>-<метка>.tar.gz`, хранятся
   три последних;
   `dev.yaml`: `previous_image = current`, `image = target`;
4. `dev up --no-open` с той же папкой;
5. `devVerify`: вход по ссылке и опрос `/api/local/status`, пока
   `runtime.state == running` (до 5 минут).

Сбой на шаге 4 или 5 → `devAutoRollback`: `down`, `localmode.Restore` из
бэкапа, `image = previous`, ошибка наружу. `agctl dev rollback
[--restore-data]` меняет `image` и `previous_image` местами и при флаге
восстанавливает dsh-home из последнего бэкапа; если платформа работала —
поднимает заново.

«Управляемые слои» dsh-home (бренд, плагины, провайдер, пресеты) при
старте на новом образе перезасеваются из скелета образа; пользовательские
настройки и сессии не трогаются.

### 2.5 Доступы и секреты

**Хранение** (`internal/localmode/secrets.go`): `Store` — обёртка над
`github.com/zalando/go-keyring` (сервис `agctl-dev`): Keychain на macOS,
Диспетчер учётных данных на Windows, Secret Service на Linux. При открытии
делается пробная запись; если хранилища нет (Linux без gnome-keyring,
WSL, CI) или `AGCTL_DEV_SECRETS=file` — зашифрованный файл
`~/.agctl-dev/secrets.enc` (AES-GCM через `internal/secrets.Manager`,
мастер-ключ 0600 рядом). Несекретные значения (`GITLAB_URL`, `JIRA_URL`,
`JIRA_USER`, произвольные с `--plain`) — в `dev.yaml: env`. Манифест
известных переменных — `localmode.Manifest`; любое другое имя вида
`[A-Z][A-Z0-9_]*` тоже допустимо.

**Что получает контейнер** (`localmode.ContainerEnv`): все `env` из
`dev.yaml` + все секреты по списку `secrets`, **кроме** `AGCTL_LLM_API_KEY`
(он идёт только серверу как провайдер dsh), плюс производные имена для
штатных инструментов, если их не задали явно:

| Из | Производные |
|---|---|
| `GITLAB_URL` | `GITLAB_HOST` (glab), `GITLAB_API_URL` = `<url>/api/v4` (MCP GitLab) |
| `GITLAB_TOKEN` | `GITLAB_PERSONAL_ACCESS_TOKEN` (MCP GitLab) |
| `JIRA_API_TOKEN` | `JIRA_PERSONAL_TOKEN` (mcp-atlassian) |
| `JIRA_USER` | `JIRA_USERNAME` |

**Почему ещё и файлы.** Инструмент bash в dsh запускает команды агента с
вычищенным окружением: переменные, в имени которых есть `KEY`, `TOKEN`,
`SECRET`, `PASSWORD`, до агента не доходят. Поэтому
`internal/server/agent_creds.go` (`writeAgentCredentials`, вызывается из
`ensureChat` и в серверном режиме тоже) кладёт доступы файлами в dsh-home
владельца, каталог `/home/dsh/.agctl` (0700, файлы 0600):

| Файл | Что | Как находит инструмент |
|---|---|---|
| `credentials.env` | все `KEY=value` (shell-quoted) | `AGCTL_CREDENTIALS_ENV`; агенту об этом говорит скилл `agctl-workspace-access` (`writeAccessSkill`), значений не раскрывая |
| `git-credentials` + `gitconfig` | credential helper `store` для `oauth2:<token>@<gitlab-host>` | `GIT_CONFIG_GLOBAL` → `git push` в GitLab по токену |
| `glab/config.yml` | хост, токен, протоколы | `GLAB_CONFIG_DIR` |
| `bridge.json` | `{url, token}` моста | `agctl bridge` читает, если env вычищен (`bridgeFromFile`) |

Пустой env → каталог удаляется (доступ отозван). MCP-серверы GitLab/Jira
получают доступы напрямую из per-workspace пресета
(`internal/provision/mcp_preset.go`, `PassEnv`).

**Ключ модели**: `devServe` ставит `AGCTL_LLM_API_KEY` в своё окружение →
`cli.Serve` собирает `provision.Provider` из профиля (`default_base_url`,
`default_models`) и ключа → `resolveDefaultLLM` сохраняет его зашифрованным
в SQLite → при каждом старте контейнера `RefreshProvider` пишет провайдера
в dsh-home. Смена ключа из лаунчера — `setDefaultLLMKey` без рестарта
сервера; контейнер подхватит при следующем старте.

Куда секреты **не** попадают: в образ, в `dev.yaml`, в логи
(`Store` значения не логирует; журнал моста без stdout), в git
(`/workspace` — папка разработчика, файлы доступов лежат в dsh-home). Env
контейнера виден через `docker inspect` только тому, у кого есть доступ к
Docker этой машины, то есть самому разработчику.

### 2.6 Мост к хосту

Задача: агент в Linux-контейнере, а часть инструментов есть только на
машине (платформа и конфигуратор 1С на Windows). Мост даёт агенту ровно те
команды, которые разработчик (или профиль организации) внёс в белый
список.

**Модель** (`internal/localbridge/bridge.go`): `Tool{Name, Description,
Command []string, Args map[string]Arg, Dir, TimeoutSec, Confirm}`;
`Arg{Description, Pattern, Required, Default}`. Имена инструментов и
аргументов — `[a-z][a-z0-9_]*`; `Command` — argv-шаблон с плейсхолдерами
`{{имя}}`, исполняемый файл плейсхолдером быть не может. `Validate`
проверяет всё это при загрузке `dev.yaml` и профиля.

**Подстановка** (`Tool.Expand`): значение аргумента целиком становится
частью **одного** элемента argv, shell не участвует. Без `pattern` значение
не может содержать управляющие символы и начинаться с `-` (защита от
подстановки опций); `pattern` якорится целиком `^(?:…)$`; длина до 1024.

**Вызов** (`Bridge.Call`): инструмент с `confirm: true` попадает в
`pending` и ждёт решения человека до `ApprovalTimeout` (5 минут): CLI
`agctl dev bridge pending` показывает ID, argv и время, `approve ID` /
`deny ID` отправляют вердикт через ControlToken. Затем `run`: `exec.Command`
без shell, рабочая папка `Dir` (с `~`), таймаут `timeout_sec` (по умолчанию
2 минуты), stdout и stderr обрезаются до 64 КиБ каждый. Результат —
`Result{ExitCode, Stdout, Stderr, DurationMS, Error}`.

**Журнал** `logs/bridge.log` (JSON lines): события `pending`, `denied`,
`approval-timeout`, `call` с argv, кодом выхода и длительностью; вывод
команд не пишется (там могут быть данные проекта). Сервер дополнительно
пишет аудит `local.bridge.call` в SQLite.

**HTTP-API на сервере** (`internal/server/local.go`): `GET
/api/local/bridge/tools` и `POST /api/local/bridge/call` под BridgeToken;
`pending` и вердикты — под ControlToken. Адрес для контейнера —
`http://host.docker.internal:<порт UI>`: на Docker Desktop это loopback
хоста; на Linux с Docker Engine `host.docker.internal` указывает на шлюз
`docker0`, поэтому `devBridgeListener` поднимает второй слушатель на этом
IP и отдаёт на нём только пути моста (UI и вход остаются на loopback).

**Клиент в контейнере** (`cmd/agctl/bridge.go`, `internal/localbridge/client.go`):
`agctl bridge list` (JSON инструментов), `agctl bridge call ИНСТР k=v …`,
`agctl bridge mcp` — stdio-сервер MCP (`ServeMCP`: `initialize`, `ping`,
`tools/list`, `tools/call`), под которым мост виден в консоли воркспейса
как «Мост к хосту» (`provision.PlatformMcpServers`, запись с
`LocalOnly: true`). Адрес и токен — из env или из `bridge.json`.
Скилл `agctl-host-bridge` (`localbridge.SkillMarkdown`) перечисляет агенту
инструменты и аргументы.

**Белый список по умолчанию из профиля**: `local/bridge-tools.yaml` с
ключом `tools` (пример — `profiles/utmn/local/bridge-tools.yaml`: `ibcmd
infobase config check` только чтение, `… import` с подтверждением).
`dev init` копирует его в `dev.yaml`, дальше разработчик правит у себя.

### 2.7 Образы рабочего места

Образ один для всех режимов — `agctl-dsh` (`internal/assets/image/Dockerfile`,
встроен в бинарь через `go:embed`):

| Стадия | Что |
|---|---|
| `base` | `node:22-bookworm-slim`, python3 + pip + pytest, make/g++, git, openssh-server, iptables |
| `dsh` | `@deepseek-ai/dsh` закреплённой версии (сейчас 0.2.2-rc.3, тег образа `0.2.2`), системный git-идентификатор агента |
| `mcp-warm` | прогрев npx-кэша MCP-серверов (DuckDuckGo, Context7, GitLab, Playwright) в `/opt/npm-mcp` |
| `runtime` | кэш MCP, LibreOffice Impress (превью pptx), ripgrep / fd / jq / sqlite3 / клиенты БД, `glab`, `jira-cli`, `uv`/`uvx` (версии в `ARG`), бинарь `agctl` (linux-сборка), скелет DSH_HOME, `entrypoint.sh`, `ax-task-runner`, `HEALTHCHECK` |
| `flavors/web` | `FROM agctl-dsh` + Chromium через Playwright |

Цепочка имён и кто её собирает:

```
agctl-dsh:0.2.2                     базовый — встроенный контекст
agctl-dsh-web:0.2.2                 вкус web — встроенный flavors/web
agctl-dsh[-web]-<профиль>:0.2.2     надстройка профиля — images.dockerfile профиля
agctl-dsh-<профиль>-<вкус>:0.2.2    вкус профиля — <профиль>/image/flavors/<вкус>/Dockerfile
```

В репозитории её собирает `make img_build [PROFILE=<id>]` (`bin/img.mk`) и
публикует `make img_push REGISTRY=…`. На машине без репозитория —
`devPrepareImage` (`cmd/agctl/dev.go`) → `dockerrt.BuildDevImage`
(`internal/runtime/dockerrt/devbuild.go`): сначала `docker pull`, а если
реестр образа не знает — сборка по плану `DevBuildPlan`, пропуская уже
готовые ступени; первый раз 10–20 минут (npm, apt, браузеры), дальше из
кэша Docker. Базовой ступени нужен `agctl-guest` под архитектуру демона
(в образе он `/usr/local/bin/agctl`): `findLinuxBinary` ищет
`agctl-guest-linux-<arch>` в `./dist`, в папке самого бинаря и в
`<папка бинаря>/../lib/agctl` (туда его кладут `make install` и
`bin/install.sh`; с GitHub это архив `agctl-guest_linux_<arch>`). Имя
`agctl-linux-<arch>` — сборка единого бинаря до разделения — тоже
подходит, пока установка не обновлена.

Выбор образа: `images.dev` профиля (`internal/profile`, поле `Images.Dev`)
— каталог лаунчера; `images.dockerfile` — надстройка; `image/flavors/*` —
вкусы. Лаунчер прячет варианты `nossh`: локально SSH не нужен.
Тот же серверный код `dockerrt.EnsureImage` (`buildBase` / `buildOverlay`)
умеет собрать базовый образ и надстройку и в серверном режиме.

### 2.8 Что в локальном режиме выключено или отличается

Серверные модули (`modules.go`) локально не подключены: нет регистрации
(`POST /api/register` → 405), ролей, лаунчера developer'а, админки,
prewarm-пула, autosave (`/workspace` — настоящие репозитории, автокоммиты
недопустимы), `/w/{id}`, Jira-поллера. Это проверяет
`TestLocalModeHasNoServerModules`. Остальное — ветки `s.isLocal()`
(`cfg.Local != nil`) в ядре:

| Где | Что |
|---|---|
| `projects.go` `handleProjectDelete` | 409: папки с диска из интерфейса не удаляются (dsh только убирает воркспейс из списка) |
| `project_mcp.go` | MCP-сервер «Мост к хосту» (`LocalOnly`) виден только локально |
| `local.go` `localChatExtras` | `/workspace` = папка разработчика, env доступов и мост — только владельцу |
| `devServe` | `--chat-ttl-min 720`: контейнер не засыпает на обеде; `--dev-env=` пустой: хостовый `.env` не пробрасывается |
| `provision/local_workroot.go` | `SetLocalWorkRoot`: проекты/воркспейсы владельца резолвятся в его папку, а не в `chats/u1/work` |
| `local_ui.go` `handleLocalWorkDirPut` | `PUT /api/local/ui/workdir`: папка проектов меняется из лаунчера (`localmode.SetWorkDir` пишет `dev.yaml` и `run/state.json`), новый маунт — со следующего старта рабочего места |
| `runtime.ChatSpec.Local` → `provision.EnsureChatPluginsLocal` | плагин «Кабинет» не ставится и вычищается с тома (node_modules, `package.json`, `cordis.patch.yml`) |
| env `AGCTL_LOCAL_MODE=1` → плагин `chat-session-files` | перед регистрацией папок снимает из реестра dsh воркспейсы под `/workspace`, чьих папок больше нет (`workspaceRegistry.delete` сохраняет сессии): слева только подпапки текущего маунта |
| `project_files.go`, `/api/me` `localMode` → плагин `workspace-console` | файлы проекта (`/api/projects/{name}/files`, `/file`) → 404; в «Возможностях» нет блока «Файлы», на карточках в чате нет «Скачать» |

Квоты LiteLLM (`quotaAwareProvider` в `ensureChat`) остаются в ядре, но
локально выключены: `--litellm-quotas` не задаётся.

### 2.9 Безопасность локального режима

- Слушает только loopback: `devInit --addr` и `devServe` отвергают любой
  другой адрес (`isLoopbackAddr`); исключение — слушатель моста на `docker0`
  (Linux), и на нём только пути моста.
- Вход — одноразовый токен из файла 0600; пароля у владельца нет (случайный,
  никому не известный). Ручки управления и моста — отдельные токены,
  сравнение константного времени.
- Ручки лаунчера — cookie-сессия + same-origin на мутации
  (`sameOriginAuthority`); значения секретов в ответах никогда не
  возвращаются.
- Мост: argv без shell, проверка значений, `confirm` + журнал + аудит,
  таймауты, лимит вывода; инструменты только из белого списка, который
  лежит в файле разработчика.
- Папка проектов — только явно выбранная, не `$HOME` и не `/`.
- Секреты — хранилище ОС или AES-GCM-файл; в образ, `dev.yaml`, логи и git
  не попадают. Кто имеет доступ к Docker машины, тот видит env контейнера —
  это сам разработчик.
- Ограничение: агент ходит в GitLab/Jira с правами человека. Правила —
  ветки `agent/*`, MR вместо push в `main`, человек мержит.

## 3. Реализация: файлы локального режима

### 3.1 `cmd/agctl` — бинарь разработчика, и `internal/cli`

| Файл | Что |
|---|---|
| `main.go` | диспетчер: `dev` → `cmdDev`, `backlog`, `completion`, `version`, `help` |
| `dev.go` | весь `agctl dev`: `devInit`, `devUp`, `devServe` (→ `cli.Serve` с `LocalMode`), `devBridgeListener`, `dockerGatewayIP`, `devDown`, `devStatus`, `devOpen`, `devLogs`, `devSecrets`, `devBridge`, `devUpdate`, `devVerify`, `devAutoRollback`, `devRollback`, `devDoctor`, `devPrepareImage`, `devDefaultImage`, `devFlavorOf`, `profileBridgeTools`, `openBrowser` |
| `dev_hooks.go` | `devHooks` — реализация `server.LocalHooks` для лаунчера: доступы (`Entries`, `SetValue`, `DeleteValue`, `ContainerEnv`, `LLMKey`, `StoreBackend`), образы (`Images`, `CurrentImage`, `SetImage`, `EnsureImage`); `dev.yaml` перечитывается на каждую операцию, потому что его параллельно правит CLI |
| `detach_unix.go`, `detach_windows.go` | отвязка фонового `dev serve` от терминала |
| `usage.go`, `completion.go` | справка (`cli.Usage`) и fish-автодополнение `dev …` |

Клиент моста внутри контейнера — `cmd/agctl-guest/bridge.go`: `agctl bridge
list \| call \| mcp` в образе; адрес и токен из env или
`$DSH_HOME/.agctl/bridge.json`.

`internal/cli` — общее для точек входа: `Serve` / `ServeOptions` /
`Runtime` (`serve.go`), `~/.config/agctl/config.yaml` серверного CLI
(`config.go`; локальный режим его отключает через `AGCTL_CONFIG`),
подготовка DSH_HOME вне Docker (`chathome.go`), справка (`usage.go`),
сведения о сборке (`build.go`), хелперы флагов и env (`util.go`).

Тесты: `dev_test.go` (доступы, белый список моста из профиля, выбор образа
по вкусу), `usage_test.go` и `completion_test.go` (нет серверных команд),
`internal/cli/*_test.go`.

### 3.2 `internal/localmode` — состояние режима на хосте

| Файл | Что |
|---|---|
| `config.go` | `Home()`, `DefaultAddr = 127.0.0.1:8930`, `Config` ↔ `dev.yaml` (`Load`, `Save`, `defaults`), `ValidateWorkDir` |
| `state.go` | `State` ↔ `run/state.json` (`WriteState`, `ReadState`, `ClearState`, `ClearOwnState`), пути (`LogPath`, `BridgeJournalPath`, `ServerHome`, `DshHome`), `NewToken`, бэкапы dsh-home (`Backup`, `Backups`, `Restore`, `pruneBackups` — три последних) |
| `secrets.go` | `Manifest` переменных, `Store` (go-keyring или `secrets.enc`), `ContainerEnv` с производными именами |
| `entries.go` | `Entry` для экрана «Доступы» и `dev secrets list`; `SetValue`, `DeleteValue`, `SetImage` с мьютексом правок `dev.yaml` |
| `keyring_name.go` | человекочитаемое имя хранилища ОС для подписей (`Keychain macOS`, `Диспетчер учётных данных Windows`, `Secret Service`) |

Тесты: `localmode_test.go`, `entries_test.go` (файловое хранилище,
производные env, бэкап/restore).

### 3.3 `internal/localbridge` — мост

`bridge.go` — `Tool`, `Arg`, `Validate`, `Expand`, `Bridge` (`Call`,
`PendingList`, `Decide`, `run`, `log`), `SkillMarkdown`; `client.go` —
`Client` (`List`, `Call`), пути `/api/local/bridge/tools|call`, env
`AGCTL_BRIDGE_URL|TOKEN`, `ServeMCP`. Тесты `bridge_test.go`: инъекции в
argv, `pattern`, `confirm`/deny/timeout, журнал.

### 3.4 `internal/server` — серверная часть локального режима

| Файл | Что |
|---|---|
| `local.go` | `LocalMode`, `localState`, `isLocal`, `initLocal`, `registerLocalRoutes`, токены (`requireControl`, `requireBridge`), `handleLocalLogin`, `handleLocalStatus`, `handleLocalShutdown`, ручки моста, `localChatExtras`, `writeLocalBridgeSkill` |
| `local_ui.go` | `LocalHooks` (интерфейс), `LocalImage`, `localTask`, `registerLocalUIRoutes`, `requireLocalOwner`, `handleLocalUI`, `handleLocalSecretPut/Delete`, `afterLocalSecretChange`, `handleLocalImagePut`, `handleLocalStart/Restart`, `startLocalRuntime` |
| `agent_creds.go` | файлы доступов в dsh-home + скилл `agctl-workspace-access` (работает и на сервере для `DevEnv` developer'а) |
| `chat.go` | общий `ensureChat`; локальные дополнения — вызовы `localChatExtras`, `writeAgentCredentials`, `writeLocalBridgeSkill` |
| `handlers.go`, `projects.go`, `project_mcp.go`, `server.go` | ветки `isLocal()` из §2.8 |

Тесты: `local_test.go` (вход по токену и 403 регистрации, токены управления
и моста, папка проектов в spec и запрет удаления, отсутствие локальных
маршрутов на сервере, MCP моста только локально), `local_ui_test.go`
(лаунчер и секреты, чужой Origin и аноним, сборка недостающего образа перед
стартом), `agent_creds_test.go`.

### 3.5 Остальное

| Где | Что |
|---|---|
| `internal/provision/local_workroot.go` | подмена work-корня владельца |
| `internal/provision/mcp_preset.go` | каталог платформенных MCP (`PlatformMcpServers`): DuckDuckGo, Context7, GitLab, Jira (`uvx mcp-atlassian`), Playwright, «Мост к хосту» (`LocalOnly`); генерация per-workspace пресета dsh с `PassEnv` |
| `internal/runtime/dockerrt/devbuild.go` | `DevBuild`, `DevBuildPlan`, `BuildDevImage`, `buildFromFS` |
| `internal/runtime/dockerrt/local.go` | тонкие помощники без `Runtime`: `PingDaemon`, `ImageID`, `PullImage` |
| `internal/runtime/dockerrt/chat.go` | `EnsureChat`: `spec.WorkDir` → `/workspace` |
| `internal/runtime/chat.go` | контракт `ChatSpec` (поля `WorkDir`, `DevEnv`, `Image`, `EgressAllowlist`, `SSHPubKey`…) и `ChatProvider` |
| `web/src/LocalLauncher.tsx`, `web/src/api.ts` (`local`), `web/src/App.tsx` | лаунчер локального режима и переключение на него |
| `internal/assets/image/`, `internal/assets/dsh-skel/` | контекст образа и скелет DSH_HOME, встроенные в бинарь (`internal/assets/assets.go`) |
| `profiles/<id>/local/bridge-tools.yaml`, `images.dev`, `images.dockerfile`, `image/flavors/*` | вклад профиля организации в локальный режим |
| `bin/install.mk` | `make install` / `uninstall` из исходников |
| `bin/install.sh`, `bin/install.ps1` | установка из GitHub Releases одной командой (`curl \| sh`, `irm \| iex`): ОС и архитектура, скачивание через `gh` или напрямую, SHA-256, `agctl` + `agctl-guest` (бинарь образа), карантин / Unblock-File, PATH; `AGCTL_RELEASE_DIR` — прогон на локальных архивах без релиза; `AGCTL_REPO=<org>/agctl-profile-<id>` — комплект организации из приватного репозитория профиля (zip `agctl-<id>-<версия>-<os>-<arch>.zip` + `checksums-<версия>.txt`, собирает `bin/release.sh` репозитория профиля): `agctl`, `agctl-guest-linux-<arch>` (старые комплекты — `agctl-linux-<arch>`) и профиль в `~/.local/share/agctl/profiles/<id>` (Windows: `%LOCALAPPDATA%\agctl\profiles\<id>`), в конце — авто-init профиля (`agctl dev init --profile-dir … --yes`; `AGCTL_NO_INIT=1` — напечатать команду, `AGCTL_WORKDIR` — папка проектов) |
| `bin/e2e-local-mode.sh` | живой прогон на изолированном `AGCTL_DEV_HOME`: init → secrets (файловое хранилище) → up → вход по токену → рабочее место с родной папкой и env → мост (вызов, подтверждение, MCP) → update на другой тег с бэкапом → rollback → down |

## 4. Удалённый (серверный) режим: что там и где в коде

Серверный режим — то, из чего локальный вырос; его код составляет большую
часть `internal/server`. Коротко, чтобы читать репозиторий:

**Вход и роли** (`handlers.go`, `roles.go`, `internal/auth`, `internal/store/roles.go`,
миграция `011_role_requests.sql`): регистрация (первый аккаунт — admin,
лимит регистраций с IP), пароли argon2id, cookie-сессии; роль `developer`
— заявка `POST /api/role-requests` → апрув админом → `users.role`.
Developer получает sshd в том же контейнере (`PUT /api/me/ssh`, порт
`:2222` публикуется только на loopback хоста; `agctl connect`, `agctl sync`
с Mutagen — `cmd/agctl-server/connect.go`, `sync.go`).

**Менеджер рантаймов** (`chat.go`): карта `userID → chatRuntime`, ленивый
`ensureChat` с per-user lock, TTL-reaper (`reapIdleChats`, 60 минут, per-user
лимиты из админки), смена образа на живом рантайме, prewarm-пул
(`chat_prewarm.go`: N последних активных пользователей держатся тёплыми).
Контракт `runtime.ChatProvider` — один для docker / k8s / ax; выбор —
`cmd/agctl-server/runtime_select.go` (`buildServer`, реализует `cli.Runtime`).

**Шлюзы**: `/chat/` (`chatProxy` в `chat.go`: reverse proxy, mint
boot-токена dsh через `mintTransport`, страница прогрева, чат-шелл
`chatshell.go` с pre-paint инъекцией бренда, `htmlscan.go`); legacy
`/w/{id}/` (`gateway.go`, `workspace_page.go`) для старых workspace-мест,
чьё создание отвечает 410.

**Лаунчер developer'а** (`launcher.go`, `web/src/Launcher.tsx`): каталог
образов `images.dev` профиля или `--dev-images`, старт/стоп/логи своего
контейнера, желаемый образ — метка `agctl.image.desired`.

**Админка** (`admin.go`, `admin_sessions.go`, `internal/store/admin.go`,
`activity.go`, миграция `012_admin_panel.sql`, `web/src/AdminPanel.tsx`,
`charts.tsx`): пользователи, аудит, обзор, контейнеры (stop/start/restart/
logs), журнал проксированных запросов `proxy_requests`, квоты токенов
`quotas`, траектории агента (чтение сессий dsh из томов), лимиты per-user.
`litellm.go` — enforcement квот виртуальными ключами LiteLLM
(`--litellm-quotas`).

**Проекты и воркспейсы** (`projects.go`, `project_files.go`,
`project_components.go`, `project_mcp.go`, `catalog.go`,
`internal/provision/projects.go`, `catalog.go`, `components.go`,
`mcp_preset.go`): папки проектов как нативные DSH Workspaces, каталог
«возможностей» (скиллы/шаблоны/инструменты из профиля), консоль
компонентов, MCP per-workspace, системный промт; go-git autosave каждые
2 минуты локальным коммитом без push (`autosaveLocalProjects`). `kits.go`,
`provision.go` — legacy-провиженинг seats (bare `repos/` + клон),
оставлен для старых записей.

**Модули сервера** (`modules.go`, `server.ServerModules()`): регистрация,
роли и SSH developer'а, лаунчер developer'а, админка, prewarm, autosave,
legacy workspaces и `/w/{id}`, Jira-поллер, конфиг раннера AX. Каждый модуль —
свои маршруты и фоновые задачи; подключает их серверная точка входа.

**Фон** (`server.go` `StartBackground`): ядро — reaper, reconcile
unhealthy-рантаймов (Docker `HEALTHCHECK`), чистка сессий; модули — autosave,
prewarm, `jirapoll.go` (скелет поллера Jira).

**Образ и сеть** (`internal/runtime/dockerrt`): `docker.go` — клиент,
`EnsureImage` (`buildBase` / `buildOverlay`), `Start` (маунты, лимиты,
публикация портов на 127.0.0.1, `host-gateway`, DNS-пин провайдера),
`egress.go` — opt-in egress-allowlist (выделенная сеть + `DOCKER-USER`),
`admin.go` — список контейнеров для админки, свип дрейфа версий.

**Кластер** (`internal/runtime/k8srt`, `internal/runtime/axrt`,
`cmd/agctl-guest/axrunner*.go`, `axguest_*.go`, `cmd/agctl-server/axegress.go`, `deploy/`): под
на пользователя (Pod + Service + NetworkPolicy + Secret + PVC/RWX) или
актор Agent Substrate под Google AX с `agctl ax-runner` (бинарь образа
`agctl-guest`) как PID 1; init-контейнер пода k8s — `agctl init-chat-home`
из того же бинаря
(fetch-at-boot конфига провайдера, egress-инъекция ключей, `ax ssh`
opt-in). Манифесты `deploy/k8s` (kustomize), `deploy/ax`, образ шлюза
`deploy/serve.Dockerfile` (`agctl-server`), смоук `bin/e2e-k8s-kind.sh`, цели `bin/k8s.mk`.
Подробности — архив `agctl-hq/docs/kubernetes.md`.

**Профиль организации** (`internal/profile`): `profile.yaml` + ассеты —
бренд, LLM-дефолты, `images`, каталог, подпись LLM на карте
(`map.llm_label`); встроенный нейтральный `agctl`
(`internal/profile/profiles/agctl`), профили организаций — отдельные репо в
`profiles/<id>` (`make profile-fetch`). Гард-тест
`TestCoreIsOrganizationFree` следит, чтобы в ядре не было литералов
организаций. Инъекция `window.__AGCTL_BRAND__` в `<head>` — единственный
канал бренда в браузер.

## 5. Карта репозитория

```
agctl/
├── cmd/
│   ├── agctl/            бинарь разработчика: dev, backlog            ← только локальный
│   ├── agctl-server/     сервер организации: serve, migrate, connect, sync, рантаймы k8s/ax
│   └── agctl-guest/      бинарь образа agctl-dsh: bridge, ax-runner, init-chat-home
├── internal/
│   ├── cli/              общее точек входа: Serve, config.yaml, справка, DSH_HOME
│   ├── server/           HTTP-сервер: API, шлюзы, рантаймы, админка, лаунчеры, локальный режим
│   ├── runtime/          контракт ChatProvider; dockerrt | k8srt | axrt
│   ├── localmode/        dev.yaml, state.json, секреты, бэкапы            ← только локальный
│   ├── localbridge/      мост к хосту: белый список, вызовы, MCP-клиент   ← только локальный
│   ├── provision/        dsh-home, проекты, каталог, MCP-пресеты, legacy seats
│   ├── store/            SQLite (modernc, без cgo) + миграции 001–013
│   ├── auth/             argon2id, cookie-сессии, rate-limit регистраций
│   ├── secrets/          AES-256-GCM с мастер-ключом
│   ├── profile/          профиль организации (данные, не код) + guard-тест
│   ├── assets/           go:embed: контекст образа, скелет DSH_HOME, шаблоны
│   └── backlog/          `agctl backlog` — карточки задач
├── web/                  React SPA (вход, лаунчеры, админка) → web/dist → go:embed
├── dsh-plugins/          плагины dsh: chat-brand, cabinet, chat-session-files, workspace-console
│                         (зеркалятся в internal/assets/dsh-skel, pre-commit проверяет синк)
├── profiles/             профили организаций (git-подмодули/клоны, не в ядре)
├── deploy/               k8s (kustomize base + overlays), ax, serve.Dockerfile
├── system/map/           карты archify (*.architecture.json → *.html), index.html, embed → /map
├── bin/                  make-модули (*.mk), e2e-скрипты, map-check.py
├── tests/e2e-ui/         браузерные сценарии (Playwright), частично до v3
├── backlog/              карточки open/done
└── docs/                 этот документ
```

Принадлежность пакетов режимам и объём (строки Go/TS без тестов, ≈):

| Пакет | Режим | ≈ строк | Назначение |
|---|---|---|---|
| `cmd/agctl` | локальный | 1710 | бинарь разработчика: `agctl dev` |
| `cmd/agctl-server` | сервер + кластер | 1010 | `serve` с модулями и выбором рантайма, `connect`, `sync` |
| `cmd/agctl-guest` | в образе | 730 | мост в контейнере, раннер AX, init-контейнер k8s |
| `internal/cli` | все | 860 | общий `serve`, конфиг, справка |
| `internal/server` | все | 7270 | ядро; `local.go` + `local_ui.go` + `agent_creds.go` ≈ 790 — локальный; `modules.go` + админка, роли, лаунчер, prewarm, LiteLLM, `/w` — сервер |
| `internal/localmode` | локальный | 880 | состояние и секреты на хосте |
| `internal/localbridge` | локальный | 610 | мост к хосту |
| `internal/runtime/dockerrt` | локальный + сервер | 1460 | Docker-адаптер; `devbuild.go`, `local.go` — локальный |
| `internal/runtime/k8srt` | кластер | 1120 | под на пользователя |
| `internal/runtime/axrt` | кластер | 1080 | актор AX / Substrate |
| `internal/provision` | все | 2530 | dsh-home, проекты, каталог, MCP; `provision.go`, `kits.go` — legacy seats |
| `internal/store` | все | 1240 | SQLite |
| `internal/profile` | все | 400 | профиль организации |
| `internal/auth`, `internal/secrets` | все | 330 | пароли, сессии, шифрование |
| `internal/backlog` | инструмент | 680 | карточки |
| `web/src` | все | 1950 | SPA: `LocalLauncher.tsx` — локальный, `Launcher.tsx` / `AdminPanel.tsx` — сервер |

Схема SQLite (миграции `internal/store/migrations`): `001_init` (users,
sessions, workspaces, secrets, audit_log), `002_template`, `003_models`, `004_jira_runs`,
`005_workspace_components`, `006_workspace_ssh`, `007_workspace_requests`,
`008_local_connectors` (старый коннектор, таблица удалена в `013_drop_local_connectors`), `009_settings` (KV: SSH-ключи,
желаемый образ, провайдер), `010_workspace_base_urls`, `011_role_requests`,
`012_admin_panel` (proxy_requests, quotas). В локальном режиме реально
используются users (один), sessions, audit, settings.

Сборка и проверки (`Makefile` + `bin/*.mk`, полный список — `make help`):

| Цель | Что |
|---|---|
| `make install` / `uninstall` | SPA + `agctl` + `agctl-server` → `~/.local/bin`, `agctl-guest` → `~/.local/lib/agctl`, fish (без исходников — `bin/install.sh` / `install.ps1` из релиза) |
| `make go_build`, `go_linux`, `go_test`, `go_vet`, `go_fmt` | `agctl` и `agctl-server`, `agctl-guest` под linux, тесты |
| `make web_build` | Vite → `web/dist` (закоммичен) → `go:embed` |
| `make img_build [PROFILE=…]`, `img_profile`, `img_push REGISTRY=…` | образы рабочего места и надстройки профиля |
| `make release_local` | `agctl` — darwin / linux / windows, `agctl-server` — darwin / linux, `agctl-guest` — linux; × arm64 / amd64 в `dist/` |
| `make up`, `corp`, `stop`, `down`, `logs`, `demo` | серверный режим на ноутбуке (`:8929`; `demo` — `:8930`, конфликтует с `agctl dev`) |
| `make profile-fetch`, `profiles` | профили организаций |
| `make k8s_*` | кластер |
| `make map`, `map_build`, `map_check` | карты системы |
| `make hooks` | pre-commit: gofmt, vet, `node --check`, синк плагинов, блок `.env` |

CI (`.github/workflows/ci.yml`): `test` на матрице ОС, `web`, `gitleaks`;
по тегу `v*` — `release-snapshot` через goreleaser (`.goreleaser.yaml`:
архивы `agctl_<os>_<arch>.tar.gz` / `.zip`, `agctl-server_<os>_<arch>.tar.gz`, `agctl-guest_linux_<arch>.tar.gz`, `checksums.txt`, `CGO_ENABLED=0`,
`-X main.version/commit/buildDate`). `dsh-bump.yml` — еженедельный cron:
обновление версии dsh в образе с гейтом e2e-smoke (`bin/dsh-bump.sh`).

## 6. Состояние и открытые вопросы

Сделано и проверено (карточка `local-mode-v2-container-bridge`):
`init/up/down/status/open/logs/secrets/bridge/update/rollback/doctor`,
лаунчер с образами и доступами, хранилище ОС с запасным файлом, мост с
подтверждениями и MCP, сборка цепочки образов на машине без репозитория,
`make install`, автодополнение fish; `bin/e2e-local-mode.sh` на живом
Docker (macOS) зелёный, агент из чата вызывал команду хоста через мост,
MCP Playwright открывал страницу в образе `web`.

Открыто:

- образы `web` и вкусов организаций в реестре организации (Nexus):
  организация заливает образы, как подготовит (ответ от 2026-11-15); до
  этого разработчик без реестра собирает образ сам, после — адреса в
  `images.dev` профиля;
- Windows живьём: машины организации — Windows 10 с Docker Desktop (ответ
  от 2026-11-15), значит бэкенд WSL 2 и `host.docker.internal` для моста
  доступны штатно; у нас есть только кросс-сборка и `detach_windows.go`,
  команды установки для PowerShell — в README; открыты сетевые политики
  до Nexus / GitLab / Jira / LiteLLM и первый живой прогон
  `bin/e2e-local-mode.sh` на такой машине;
- модели: по умолчанию провайдер из профиля организации (ключ выдаёт
  платформа); разрешение на личные подписки разработчиков организация
  обещала запросить — технически личный провайдер в настройках dsh уже
  перекрывает платформенный;
- Jira-MCP и `jira-cli init` на реальной Jira организации;
- релизы на GitHub: конвейер goreleaser готов, тег `v*` ещё не выпускался;
- опциональный control plane сервера для локального режима (персональные
  ключи LiteLLM, централизованные пины версий, напоминание об обновлении)
  — после пилота;
- нативный режим без Docker (dsh на хосте) — запасной вариант, не
  реализован.
