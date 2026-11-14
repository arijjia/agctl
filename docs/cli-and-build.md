# agctl: сборка, make, CLI и автодополнение

Справочник «от исходников до команды в терминале»:
- из чего состоит бинарь `agctl` и как он собирается;
- что делает каждая цель `make`;
- все команды CLI, включая локальный режим `agctl dev`;
- как пользоваться автодополнением fish.

Сам локальный режим (доступы, мост к хосту, обновления) подробно описан в
[local-mode.md](local-mode.md). Архитектура платформы — в
[architecture.md](architecture.md).

---

## 0. «неизвестная команда "dev"» — что это значит

Команда `dev` есть в любом agctl, собранном после 13.11.2026 (мерж d8e0e3e).
Если `agctl dev …` отвечает «неизвестная команда», значит в PATH лежит
**старый бинарь**: его когда-то скопировали в `~/.local/bin`, а потом не
обновили. `make go_build` собирает только `dist/agctl` в репозитории и PATH
не трогает.

Проверить, какой бинарь запускается:

```fish
type -a agctl      # где лежит (fish); в bash/zsh: which -a agctl
agctl version      # agctl 0.1.0 <коммит> <дата> (darwin/arm64 …)
```

Начиная с этой версии `agctl version` печатает коммит и дату сборки. У
старых бинарей там только `0.1.0`, так их и можно отличить.

Как починить: из корня репозитория выполнить

```bash
make install
```

Цель собирает SPA, бинарь и linux-сборку и ставит:

| Что | Куда |
|---|---|
| бинарь | `~/.local/bin/agctl` |
| linux-сборка (для образа) | `~/.local/lib/agctl/agctl-linux-<arch>` |
| автодополнение fish | `~/.config/fish/completions/agctl.fish` |

Если первым в PATH окажется другой `agctl`, `make install` об этом
предупредит. После каждого `git pull` повторяйте `make install`.

---

## 1. Локальный режим v2 — как он устроен сейчас

Коротко: **это тот же бинарь `agctl` и тот же сервер, что на продакшене**, но
запущенный на машине разработчика для одного человека.

```
 браузер ──127.0.0.1:8930──▶ agctl dev serve (фон, 1 Go-процесс)
                               │  SQLite + dsh-home в ~/.agctl-dev/data
                               │  Docker API
                               ▼
                        контейнер agctl-dsh[-web|-utmn-1c]  (локальный Docker)
                          dsh web · glab · jira · uv · Playwright | Java+BSL LS
                          /workspace  ◀── bind mount ── ваша папка проектов
                          agctl bridge ──HTTP+токен──▶ мост к хосту ──exec──▶ ibcmd и др.
                          ──HTTPS──▶ GitLab / Jira / LLM
```

### Процессы

| Процесс | Где | Что делает |
|---|---|---|
| `agctl dev …` | хост | короткоживущий CLI: init, up, status… |
| `agctl dev serve` | хост, фон | обычный `serve` с `Config.Local`: один владелец `owner` с ролью developer, вход по одноразовой ссылке сразу в `/chat/`, регистрации нет. Слушает только `127.0.0.1:8930`; на Linux дополнительно — адрес `docker0`, и там только пути моста |
| контейнер рабочего места | локальный Docker | один на машину; в нём `dsh web` |

### Где что лежит

| Путь | Что там |
|---|---|
| `~/.agctl-dev/dev.yaml` | профиль, образ, вкус, папка проектов, адрес, лимиты, несекретные env (URL, логины), *имена* секретов, белый список моста, `previous_image` |
| `~/.agctl-dev/data/` | дом сервера: SQLite, dsh-home (сессии и настройки dsh) |
| `~/.agctl-dev/run/state.json` | PID, адрес, токены управления фонового процесса |
| `~/.agctl-dev/logs/serve.log`, `bridge.log` | лог сервера и журнал вызовов моста |
| `~/.agctl-dev/backups/` | 3 последних бэкапа dsh-home (делаются перед `update`) |

Дом можно перенести переменной `AGCTL_DEV_HOME`.

### Секреты

Хранятся в хранилище ОС: Keychain на macOS, Диспетчер учётных данных на
Windows, Secret Service на Linux. Если хранилища нет, используется
`~/.agctl-dev/secrets.enc` (AES-GCM); принудительно включается так:
`AGCTL_DEV_SECRETS=file`.

- **Ключ модели** (`AGCTL_LLM_API_KEY`) получает только сервер как провайдер
  dsh, в контейнер он не попадает.
- **Остальные доступы** (GitLab, Jira) передаются в env контейнера.
  Дополнительно сервер выводит из них имена, которые ждут glab и MCP:
  `GITLAB_HOST`, `GITLAB_API_URL`, `GITLAB_PERSONAL_ACCESS_TOKEN`,
  `JIRA_PERSONAL_TOKEN`, `JIRA_USERNAME`.
- **Файлы для команд агента.** Команды агента dsh запускает с очищенным
  окружением: переменные, в имени которых есть KEY, TOKEN, SECRET или
  PASSWORD, убираются. Поэтому те же доступы лежат файлами в
  `dsh-home/.agctl/` с правами 0600:
  - `credentials.env`;
  - `gitconfig` с credential helper;
  - `glab/config.yml`;
  - `bridge.json`.

  Инструменты находят эти файлы по переменным `GIT_CONFIG_GLOBAL`,
  `GLAB_CONFIG_DIR` и `AGCTL_CREDENTIALS_ENV`.

### Мост к хосту

Агент не может выполнить на хосте что угодно — только команды из белого
списка в `dev.yaml` (`bridge.tools`). Он заполняется из `local/bridge-tools.yaml`
профиля.

- Каждая команда — это `argv` без shell, со значениями, проверенными по
  шаблону.
- Инструменты с `confirm: true` ждут `agctl dev bridge approve ID`.
- Каждый вызов записывается в `logs/bridge.log`.

В контейнере мост доступен как CLI `agctl bridge list | call | mcp` и как
MCP-сервер «host-bridge».

### Образы рабочего места

`dev init` выбирает образ так:
1. `--image REF`;
2. иначе `--flavor web|1c` → образ вкуса из `images.dev` профиля;
3. иначе первый образ из `images.dev` профиля;
4. для встроенного профиля `agctl` это `agctl-dsh:0.2.2`.

Если образа нет локально, `init` пробует `docker pull`.

Если образа нет ни локально, ни в реестре, **`dev up` собирает его сам на
этой машине** — всю цепочку, как `make img_build` / `img_profile`, пропуская
уже готовые ступени (`dockerrt.BuildDevImage`):

```
agctl-dsh:0.2.2                базовый — из контекста, встроенного в бинарь
agctl-dsh-web:0.2.2            вкус web — тоже встроен
agctl-dsh[-web]-utmn:0.2.2     надстройка профиля — из profile_dir/image
agctl-dsh-utmn-1c:0.2.2        вкус профиля — из profile_dir/image/flavors/1c
```

Первый раз это 10–20 минут, потом всё берётся из кэша Docker. Для базовой
ступени нужна linux-сборка agctl под архитектуру Docker. Её ищут по
очереди:
1. `./dist`;
2. папка самого бинаря;
3. `<папка бинаря>/../lib/agctl` (туда её кладёт `make install`).

### Как собрать бинарь «под локальный режим»

Отдельной сборки нет — подходит любой свежий `agctl`. Варианты:

| Сценарий | Что сделать |
|---|---|
| своя машина, есть репозиторий | `make install`, затем `agctl dev init` и `agctl dev up` |
| своя машина, нужны образы web или 1С | `make img_build` (web) или `make img_build PROFILE=utmn` (надстройка utmn и её вкусы), затем `agctl dev init --flavor web` |
| другая машина без репозитория | `make release_local`. На ту машину скопировать `agctl-<os>-<arch>` (переименовать в `agctl`) и рядом **в ту же папку** `agctl-linux-<arch Docker>`. Образ (включая вкусы и надстройку профиля из `--profile-dir`) соберётся при первом `dev up` — минут 10–20, тянет npm, apt, PyPI и GitHub. Готовые образы из реестра быстрее: `docker login` в Nexus, затем `agctl dev init --image <реестр>/agctl-dsh-web:0.2.2` |
| организация (пилот) | `make img_build PROFILE=utmn` + `make img_push REGISTRY=<nexus>/agctl`; разработчики получают только бинарь, а образы тянут из Nexus |

---

## 2. Из чего состоит бинарь

`agctl` — один статический Go-бинарь (`CGO_ENABLED=0` в релизах). Внутри
через `go:embed` зашиты:

| Что встроено | Откуда в репо | Зачем |
|---|---|---|
| React SPA (кабинет, админка, вход) | `web/dist/` (`web/embed.go`) | UI платформы; `web/dist` **закоммичен**, поэтому `go build` работает и без Node |
| контекст образа `agctl-dsh` | `internal/assets/image/` (Dockerfile, `entrypoint.sh`, `dsh-net.mjs`, `office2pdf`, `flavors/web`) | `serve` и `dev up` могут сами собрать образ |
| скелет DSH_HOME | `internal/assets/dsh-skel/` | плагины dsh (chat-brand, cabinet…), профиль web; сеется в пустой том |
| шаблоны | `internal/assets/templates/` | шаблоны доменов и проектов |
| встроенный профиль `agctl` | `internal/profile/profiles/agctl/` | нейтральный бренд, LLM-дефолты, `images.dev` |
| миграции SQLite | `internal/store/migrations/*.sql` | схема БД; накатываются при старте |

Через ldflags при сборке подставляются:
- `main.version` — `VERSION` в Makefile (по умолчанию `0.1.0`) или тег в
  goreleaser;
- `main.commit` — короткий хеш, с суффиксом `-dirty`, если есть
  незакоммиченные правки;
- `main.buildDate` — дата сборки;
- `-s -w` убирают таблицу символов и DWARF, бинарь получается меньше.

Флаг `-trimpath` убирает из бинаря абсолютные пути машины сборщика.

Профили организаций (utmn и др.) **в бинарь не встраиваются**: ядро
публичное, и гард-тест `TestCoreIsOrganizationFree` за этим следит. Профиль
подключают с диска: `--profile-dir`, `profiles/<id>` или
`make profile-fetch`.

---

## 3. Этапы сборки — что происходит на каждом шаге

### 3.1 `make web_build` — SPA

1. `web_install`: `cd web && npm install` (React 18, Vite, TypeScript).
2. `npm run build` → `vite build` → `web/dist/index.html` +
   `web/dist/assets/*.js|css` с хешами в именах.
3. Результат попадает в бинарь на следующем `go build` через
   `//go:embed all:dist`.

Нужно, только если вы меняли `web/src`. Иначе подойдёт закоммиченный
`web/dist`.

### 3.2 `make go_build` — бинарь под эту машину

```
go build -trimpath -ldflags '-s -w -X main.version=… -X main.commit=… -X main.buildDate=…' -o dist/agctl ./cmd/agctl
```

Go компилирует `cmd/agctl` и все пакеты `internal/*` и вшивает
embed-файлы: SPA, контекст образа, скелет, профиль `agctl`, миграции. С
тёплым кэшем это занимает секунды.

### 3.3 `make go_linux` — linux-сборка для образа

`GOOS=linux GOARCH=<arch Docker-демона>` → `dist/agctl-linux-arm64|amd64`.

Архитектуру берут из `docker version --format '{{.Server.Arch}}'`: на Mac с
Apple Silicon это arm64. Этот бинарь кладётся **внутрь** образа как
`/usr/local/bin/agctl`. В контейнере он нужен для `agctl bridge`,
`agctl backlog`, `ax-runner` и `init-chat-home`.

### 3.4 `make img_build` — образы рабочего места

1. Сборка linux-бинаря (как `go_linux`).
2. Сборка контекста в `.build-img/`:
   - копия `internal/assets/image/`;
   - `skel/` ← `internal/assets/dsh-skel/`;
   - `agctl` ← linux-бинарь.
3. `docker build -t agctl-dsh:0.2.2 .build-img`. Стадии Dockerfile:
   - **base** — `node:22-bookworm-slim` + python3/pytest/pip, make/g++,
     git, openssh-server, iptables, npm@12.
   - **dsh** — системный git-идентификатор агента и `npm allow-scripts`
     для нативных зависимостей. Ставится `@deepseek-ai/dsh@0.2.2-rc.3`
     (pinned) и проверяется `dsh --version`.
   - **mcp-warm** — прогрев npx-кэша MCP-серверов (ddg, context7,
     `@zereight/mcp-gitlab@2.3.27`, `@playwright/mcp@0.0.85`) в
     `/opt/npm-mcp`. Первая сессия не ходит за ними в сеть. Если пакета нет
     в кэше, сборка падает.
   - **runtime**:
     - копия кэша MCP и `libreoffice-impress` (превью pptx);
     - инструменты разработчика: ripgrep, jq, fd, sqlite3, клиенты
       PostgreSQL/MySQL/Redis, tree, htop;
     - `glab` 1.130.0, `jira-cli` 1.7.0, `uv`/`uvx` 0.13.8 — версии в `ARG`;
     - `agctl`, скелет, entrypoint, `ax-task-runner`;
     - `HEALTHCHECK` (`agctl-dsh-net health`), `ENTRYPOINT agctl-entrypoint`.
4. `docker build --build-arg AGCTL_NO_SSHD=1 -t agctl-dsh-nossh:0.2.2`. Это
   тот же образ, только sshd никогда не поднимается. Слои из кэша, сборка
   занимает секунды.
5. Вкус **web** (если `IMG_FLAVORS` содержит `web`, по умолчанию да):
   `FROM agctl-dsh:0.2.2` + Chromium через Playwright из npx-кэша
   (`PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright`) → `agctl-dsh-web:0.2.2`.
6. `img_profile`: если у профиля `PROFILE` есть `images.dockerfile`, его
   надстройка собирается поверх agctl-dsh, nossh и web:
   `agctl-dsh-<id>:0.2.2`, `agctl-dsh-<id>-nossh:0.2.2`,
   `agctl-dsh-web-<id>:0.2.2`. Затем собираются вкусы профиля из
   `profiles/<id>/image/flavors/<вкус>/Dockerfile` → `agctl-dsh-<id>-<вкус>:0.2.2`
   (у utmn вкус `1c`: Temurin JRE 21 + BSL LS 1.0.7). `PIP_INDEX_URL=…`
   передаёт зеркало PyPI (Nexus) в стадию wheels надстройки.

Время: с тёплым кэшем — секунды. С нуля — 10–20 минут: npm, apt,
Chromium, LibreOffice.

### 3.5 Сборка образа самим сервером (`EnsureImage`)

Перед стартом контейнера `serve` (и `dev serve`) проверяет образ:
- **образа нет** → `buildBase`:
  - распаковывает встроенный контекст во временную папку;
  - находит linux-сборку под архитектуру демона;
  - собирает образ через Docker API;
- **профиль с надстройкой** → сначала базовый, потом `buildOverlay` из
  каталога профиля.

На машине без репозитория работает так же, если рядом с бинарём лежит
`agctl-linux-<arch>`.

### 3.6 Кросс-сборка и релизы

- **`make release_local`** собирает `dist/agctl` и матрицу
  `dist/agctl-{darwin,linux,windows}-{arm64,amd64}[.exe]` на одной машине:
  Go кросс-компилирует без cgo. Версия — `make release_local VERSION=0.1.1`.
- **Релиз на GitHub**: пуш тега `v*`. CI сначала гоняет jobs `test` и `web`,
  потом запускает job `release-snapshot`: goreleaser собирает ту же матрицу.
  Архивы называются `agctl_<os>_<arch>.tar.gz` (для Windows — `.zip`), к ним
  прикладывается `checksums.txt`; всё публикуется в GitHub Releases. Конфиг —
  `.goreleaser.yaml`.
- **Образ шлюза для Kubernetes**: `make img_serve_build` — distroless-образ
  `agctl-serve` (`deploy/serve.Dockerfile`).

---

## 4. Все цели make

Полный список с описаниями печатает `make help`. Переменные
переопределяются так: `make up PROFILE=utmn RUN_ADDR=127.0.0.1:9000`.
`.env` (шаблон `.env.example`) экспортируется во все цели, туда кладут
ключи LLM.

### Сборка и проверки

| Цель | Что делает |
|---|---|
| `make install` | SPA + бинарь + linux-сборка → `~/.local/bin/agctl`, `~/.local/lib/agctl/`, автодополнение fish. `INSTALL_DIR=…` меняет каталог |
| `make uninstall` | убрать то, что поставил `install`; данные не трогает |
| `make go_build` / `make build` | `dist/agctl` под эту машину |
| `make go_linux` | `dist/agctl-linux-<arch Docker>` |
| `make go_test` / `make test`, `make go_vet`, `make go_fmt` | тесты, vet, gofmt |
| `make go_run` | собрать и запустить `serve` на переднем плане |
| `make web_install`, `make web_build`, `make web_dev` | npm install; сборка SPA; Vite dev-сервер |
| `make all` | `go_build` + `web_build` + `img_build` |
| `make release_local` | матрица 3 ОС × 2 архитектуры в `dist/` |
| `make hooks` | pre-commit: gofmt, vet, `node --check`, синк плагинов, блок `.env` |

### Образы

| Цель | Что делает |
|---|---|
| `make img_build` | agctl-dsh, nossh, web и надстройка `PROFILE` (подробно — §3.4). `IMG=…`, `IMG_FLAVORS=` (пусто — без web), `PIP_INDEX_URL=…` |
| `make img_profile PROFILE=utmn` | только надстройка профиля и его вкусы поверх уже собранных базовых |
| `make img_build_nossh` | только nossh из готового `.build-img` |
| `make img_build_amd64` | agctl-dsh под linux/amd64 (с Mac для x86-сервера) |
| `make img_push REGISTRY=…` | тег и push всех собранных образов, включая вкусы и надстройки, в реестр (Nexus); печатает digest |

### Запуск платформы (серверный режим на ноутбуке)

| Цель | Что делает |
|---|---|
| `make up` | собрать всё и поднять `serve` в фоне на `127.0.0.1:8929`, дом `~/.agctl`. `PROFILE=<id>`, `RUN_ADDR`, `RUN_HOME`, `DEV_IMAGES` |
| `make corp` | то же, с корпоративным LiteLLM (`CORP_LLM_*`, ключ в `.env`) |
| `make stop` / `down` / `restart` | SIGTERM инстансам make; `down` — плюс `docker stop` контейнеров agctl |
| `make logs` / `status` | хвост лога; что запущено |
| `make demo` / `demo_fresh` / `demo_clean` | изолированный демо-стенд: `/tmp/agctl-demo`, `:8930`. **Порт совпадает с `agctl dev`** — одновременно они не поднимутся |
| `make demo-users` | тестовые роли user, developer, admin |

### Профили, Kubernetes, карта

| Цель | Что делает |
|---|---|
| `make profile-fetch PROFILE_REPO=…` | профиль организации → `profiles/<id>` |
| `make profiles` | какие профили доступны |
| `make k8s_render` / `k8s_apply` / `k8s_profile` / `k8s_secret` / `k8s_status` / `k8s_e2e_kind` | деплой и смоук в Kubernetes ([kubernetes.md](kubernetes.md)) |
| `make map` / `map_build` / `map_check` | карта системы: открыть, пересобрать (нужен archify), проверить |

### Скрипты e2e

| Скрипт | Что проверяет |
|---|---|
| `bin/e2e-local-mode.sh` | локальный режим целиком в изолированном `$TEST_HOME`: init, up, мост, секреты, update/rollback (42 проверки) |
| `bin/e2e-k8s-kind.sh` | бэкенд k8s в kind |

---

## 5. CLI agctl — все команды

`agctl help` выводит справку, `agctl dev help` — справку локального режима.

### Локальный режим: `agctl dev`

| Команда | Флаги и аргументы | Что делает |
|---|---|---|
| `dev init` | `--profile ID`, `--profile-dir DIR`, `--image REF`, `--flavor web\|1c`, `--workdir DIR`, `--addr 127.0.0.1:PORT`, `--yes`, `--cli`, `--no-pull` | профиль и папка проектов; образ и доступы — в лаунчере (подробно ниже) |
| `dev up [DIR]` | `--no-open`, `--foreground` | проверяет Docker и свободный порт, запускает фоновый `dev serve`, ждёт готовности, открывает браузер со ссылкой входа. `DIR` становится `/workspace` вместо папки из `dev.yaml` |
| `dev down` | — | остановить сервер и контейнер; данные сохраняются |
| `dev status` | — | адрес, образ, папка, контейнер, мост |
| `dev open` | — | новая одноразовая ссылка входа в браузере |
| `dev logs` | `-f` | лог `serve.log` |
| `dev secrets list` | — | какие доступы заданы и где хранятся; значения не печатаются |
| `dev secrets set ИМЯ` | `--stdin`, `--plain` | задать значение: скрыто с терминала, из stdin или несекретно в `dev.yaml` |
| `dev secrets rm ИМЯ` | — | удалить |
| `dev bridge list` / `pending` / `approve ID` / `deny ID` / `log` | — | белый список; вызовы, ждущие подтверждения; решение; журнал |
| `dev update` | `--image REF` | `pull` → бэкап dsh-home → перезапуск → проверка → автооткат при сбое |
| `dev rollback` | `--restore-data` | вернуть `previous_image`, при необходимости и dsh-home из бэкапа |
| `dev doctor` | — | Docker, образ, доступы, сеть, мост — с подсказками |

Что делает `dev init`:
1. выбирает профиль (`--profile` или `--profile-dir`);
2. спрашивает папку проектов (весь `$HOME` и `/` запрещены);
3. берёт образ из `--image`/`--flavor` или первый из профиля;
4. переносит белый список моста из профиля.

Образ и доступы (ключ LLM, GitLab, Jira) настраиваются **в лаунчере** после
`dev up`: вкладки «Рабочее место» и «Доступы» (подробно —
[local-mode.md](local-mode.md)). С `--cli` — по-старому, мастером в
терминале: номер образа, доступы, скачивание образа.

Пример полностью без вопросов:

```bash
agctl dev init --profile-dir ~/src/cit/agctl-profile-utmn --flavor 1c --workdir ~/src/1c --yes
```

### Внутри контейнера: `agctl bridge`

| Команда | Что делает |
|---|---|
| `agctl bridge list` | инструменты моста в JSON |
| `agctl bridge call ИНСТР k=v …` | вызвать команду хоста; при `confirm` ждёт подтверждения |
| `agctl bridge mcp` | MCP stdio-сервер моста; так он подключён как «host-bridge» |

Адрес и токен берутся из `AGCTL_BRIDGE_URL`/`AGCTL_BRIDGE_TOKEN`, а если их
нет — из `$DSH_HOME/.agctl/bridge.json`.

### Серверный режим и прочее

| Команда | Что делает |
|---|---|
| `agctl serve` | платформа: UI, API и runtime пользователей. Флаги `--home`, `--addr`, `--image`, `--dev-images`, `--bin-dir`, `--profile`/`--profile-dir`, `--llm-*`, `--mem-gb`, `--cpus`, `--chat-ttl-min`, `--secure-cookies`, `--public-url`, `--cors-origin`, `--tier0`, `--runtime docker\|k8s\|ax` и `--k8s-*`/`--ax-*`. Полная таблица — README, «Команды бинарника» |
| `agctl migrate --home DIR` | накатить миграции SQLite |
| `agctl seed-demo-users --home DIR` | тестовые роли |
| `agctl connect --port P [--host H] [--identity KEY] [--print]` | SSH в персональный контейнер (серверный режим) |
| `agctl sync create\|list\|pause\|resume\|flush\|terminate` | Mutagen-синхронизация папки с `/workspace` |
| `agctl backlog new\|list\|top\|check\|start\|done\|cancel\|note\|migrate` | карточки бэклога (`backlog/open`, `backlog/done`) |
| `agctl config` | эффективный `~/.config/agctl/config.yaml` |
| `agctl completion fish` | скрипт автодополнения |
| `agctl version` | версия, коммит, дата, ОС/архитектура, Go |
| `agctl local connect …` | experimental-коннектор (legacy, не путать с `dev`) |
| `agctl init-chat-home`, `agctl ax-runner` | служебные: init-контейнер K8s; PID 1 песочницы AX |

### Переменные окружения

| Переменная | Где | Значение |
|---|---|---|
| `AGCTL_DEV_HOME` | dev | дом локального режима (`~/.agctl-dev`) |
| `AGCTL_DEV_SECRETS=file` | dev | секреты в `secrets.enc` вместо хранилища ОС |
| `AGCTL_HOME` | serve | дом серверного режима (`~/.agctl`) |
| `AGCTL_ADDR`, `AGCTL_PROFILE`, `AGCTL_CONFIG`, `AGCTL_RUNTIME` | serve | адрес, профиль, путь к config.yaml, бэкенд |
| `AGCTL_LLM_API_KEY` | serve | ключ провайдера; только env или `.env`, никогда не в файлы репо |
| `NO_COLOR` | все | справка без цвета |

---

## 6. Автодополнение fish

### Установка

`make install` ставит его сам. Вручную:

```fish
agctl completion fish > ~/.config/fish/completions/agctl.fish
```

Fish подхватывает файл в новых окружениях сразу. В уже открытом терминале
выполните `source ~/.config/fish/completions/agctl.fish`. После обновления
agctl перегенерируйте файл (или снова выполните `make install`): скрипт
статический и знает только команды своей версии.

### Как пользоваться

- **Tab** — дополнить или показать варианты; описание показывается справа
  серым.
- **Tab, Tab** — список вариантов: стрелками выбрать, Enter — подставить.
- Начните флаг с `--` и нажмите Tab — увидите флаги именно этой подкоманды.
- Где ждут каталог (`dev up`, `--workdir`, `--profile-dir`), Tab дополняет
  пути. Для `--flavor` предлагаются `web` и `1c`, для
  `dev secrets set`/`rm` — известные имена.
- `Alt+→` или `Ctrl+F` принимает серую автоподсказку fish из истории. Это не
  дополнение agctl, но удобно для повтора команд.

Что предлагается на Tab:

```
agctl ⇥                  → serve dev bridge backlog connect sync config version …
agctl dev ⇥              → init up down status open logs secrets bridge update rollback doctor help
agctl dev init --⇥       → --profile --profile-dir --image --flavor --workdir --addr --yes --cli --no-pull
agctl dev init --flavor ⇥ → web 1c
agctl dev up ⇥           → каталоги
agctl dev up --⇥         → --no-open --foreground
agctl dev secrets ⇥      → list set rm
agctl dev secrets set ⇥  → AGCTL_LLM_API_KEY GITLAB_URL GITLAB_TOKEN JIRA_URL JIRA_USER JIRA_API_TOKEN
agctl dev bridge ⇥       → list pending approve deny log
agctl bridge ⇥           → list call mcp
agctl backlog new --priority ⇥ → P1 P2 P3
```

---

## 7. Типовые сценарии

**Поставить свежий agctl и поработать локально:**

```bash
make install
agctl dev init
agctl dev up ~/src/мой-проект
```

**Нужен браузер для агента (вкус web):**

```bash
make img_build
agctl dev init --flavor web
agctl dev up
```

**1С-разработка с профилем utmn:**

```bash
make img_build PROFILE=utmn
agctl dev init --profile-dir ../agctl-profile-utmn --flavor 1c
agctl dev up ~/src/1c
```

**Агент попросил выполнить команду на хосте с подтверждением:**

```bash
agctl dev bridge pending
agctl dev bridge approve <ID>
```

**Обновиться после пересборки образа:**

```bash
make img_build
agctl dev update
```

Если стало хуже — `agctl dev rollback`.

**Что-то не работает:**

```bash
agctl dev doctor
agctl dev logs -f
agctl version
```

`agctl version` нужен, чтобы убедиться, что запускается свежий бинарь.

**Собрать бинари для другой машины:**

```bash
make release_local VERSION=0.1.0
```

На ту машину скопировать `dist/agctl-<os>-<arch>` и рядом
`dist/agctl-linux-<arch>` (см. §1).
