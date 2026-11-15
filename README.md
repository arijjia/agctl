# agctl

**Агентское рабочее место на своей машине одним бинарём.** `agctl dev`
поднимает на ноутбуке разработчика платформу для одного человека: в браузере
открывается чат с агентом (DeepSeek Harness, `dsh`), агент работает в
контейнере на локальном Docker, видит только выбранную папку проектов и
ходит в GitLab, Jira и модель с доступами, которые лежат в хранилище ОС.
Клонировать репозиторий и что-то собирать для этого не нужно: достаточно
бинаря `agctl` и Docker.

Тот же бинарь умеет серверный режим (`agctl serve`: много пользователей,
роли, Kubernetes), но в этом README — только локальный сценарий. Устройство
обоих режимов и разбор кодовой базы —
[docs/local-mode-architecture.md](docs/local-mode-architecture.md).

## Что нужно

- **Docker**: Docker Desktop (macOS; Windows 10/11 с бэкендом WSL 2), Docker
  Engine (Linux), либо Colima / Rancher Desktop / Podman Desktop.
  `docker version` должен отвечать без ошибок.
- **Бинарь `agctl`** под вашу ОС — ниже два способа его получить.
- 8 ГБ ОЗУ и ~15 ГБ диска: образ рабочего места с браузерами занимает
  2–3 ГБ.
- Ключ модели (OpenAI-совместимый провайдер: LiteLLM организации или
  публичный) — выдаёт команда платформы. Свою подписку можно подключить
  в настройках dsh внутри чата: личный провайдер перекрывает платформенный.

## Получить бинарь

### Вариант 1: скачать с GitHub

Релизы собираются goreleaser по тегу `v*` и публикуются на странице
[Releases](https://github.com/arijjia/agctl/releases): архивы
`agctl_<ос>_<архитектура>.tar.gz` (для Windows — `.zip`) и `checksums.txt`.
Если список релизов пуст, тег ещё не выпускался: соберите из исходников
(вариант 2) или возьмите сборку у команды платформы. Организации могут
публиковать и собственный релиз (бинарь + профиль организации в одном
архиве) в своём приватном репозитории.

#### Одной командой

macOS и Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.sh | sh
```

Windows 10 / 11, PowerShell:

```powershell
irm https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.ps1 | iex
```

Скрипт ([bin/install.sh](bin/install.sh), [bin/install.ps1](bin/install.ps1))
определяет ОС и архитектуру, скачивает архив последнего релиза и
`checksums.txt` (через `gh`, если он установлен и авторизован, иначе
напрямую), сверяет SHA-256, кладёт бинарь в `~/.local/bin` (Windows:
`%LOCALAPPDATA%\agctl`), снимает карантин macOS или отметку «скачано из
интернета» на Windows, рядом кладёт linux-сборку для самосборки образа,
на Windows добавляет каталог в `PATH` пользователя (на macOS и Linux
подсказывает, если `~/.local/bin` в `PATH` нет) и печатает `agctl version`.
Переменные: `AGCTL_VERSION=v0.1.0` — конкретный тег, `AGCTL_INSTALL_DIR` —
другой каталог, `AGCTL_NO_LINUX=1` — без linux-сборки (образы берутся из
реестра), `AGCTL_RELEASE_DIR` — папка с уже скачанными архивами.

Дальше — раздел «Запуск». Нужны Docker Desktop (на Windows — с бэкендом WSL 2) и `gh` или `curl`.
Живьём на Windows локальный режим ещё не прогонялся, так что первый запуск
там стоит делать вместе с командой платформы.

<details>
<summary>Те же шаги вручную, по командам</summary>

#### Windows 10 / 11 (PowerShell, Docker Desktop)

Нужны Docker Desktop с бэкендом WSL 2 (Windows 10 2004+; `docker version`
в PowerShell отвечает без ошибки) и `gh` (`winget install GitHub.cli`).
Архитектура — `amd64`.

Создать каталог программы `%LOCALAPPDATA%\agctl` и перейти во временную папку для скачивания:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\agctl" | Out-Null; Set-Location (New-Item -ItemType Directory -Force "$env:TEMP\agctl-dl")
```

Скачать архив для Windows, linux-сборку (для самосборки образа) и файл контрольных сумм:

```powershell
gh release download --repo arijjia/agctl --pattern 'agctl_windows_amd64.zip' --pattern 'agctl_linux_amd64.tar.gz' --pattern checksums.txt
```

Сверить SHA-256 архива с опубликованной (должно напечатать `checksum OK`):

```powershell
$h = (Get-FileHash agctl_windows_amd64.zip -Algorithm SHA256).Hash.ToLower(); if ((Get-Content checksums.txt) -match "^$h\s") { 'checksum OK' } else { 'CHECKSUM MISMATCH' }
```

Распаковать `agctl.exe`, положить в каталог программы и снять отметку «скачано из интернета»:

```powershell
Expand-Archive -Force agctl_windows_amd64.zip -DestinationPath .; Move-Item -Force agctl.exe "$env:LOCALAPPDATA\agctl\agctl.exe"; Unblock-File "$env:LOCALAPPDATA\agctl\agctl.exe"
```

Положить linux-сборку рядом с программой под именем `agctl-linux-amd64` (`tar` есть в Windows 10 с версии 1803):

```powershell
tar xzf agctl_linux_amd64.tar.gz agctl; Move-Item -Force agctl "$env:LOCALAPPDATA\agctl\agctl-linux-amd64"
```

Добавить каталог программы в пользовательский `PATH` (навсегда и в текущее окно):

```powershell
[Environment]::SetEnvironmentVariable('Path', [Environment]::GetEnvironmentVariable('Path', 'User') + ";$env:LOCALAPPDATA\agctl", 'User'); $env:Path += ";$env:LOCALAPPDATA\agctl"
```

Проверить, что программа запускается и какой версии:

```powershell
agctl version
```

Дом локального режима — `%USERPROFILE%\.agctl-dev`, секреты уходят в
Диспетчер учётных данных Windows. Дальше, как на любой ОС:
`agctl dev init --workdir C:\src\проекты`, затем `agctl dev up`. Папка
проектов должна лежать на диске Windows, доступном Docker Desktop (по
умолчанию доступны все локальные диски). Живьём на Windows локальный режим
ещё не прогонялся: есть только кросс-сборка, так что первый запуск стоит
делать вместе с командой платформы и присылать вывод `agctl dev doctor` и
`agctl dev logs`.

#### macOS (Apple Silicon; для Intel замените `arm64` на `amd64`)

Создать каталоги для программы и linux-сборки, перейти во временную папку для скачивания:

```bash
mkdir -p ~/.local/bin ~/.local/lib/agctl && cd "$(mktemp -d)"
```

Скачать архив для macOS, linux-сборку (для самосборки образа) и файл контрольных сумм:

```bash
gh release download --repo arijjia/agctl --pattern 'agctl_darwin_arm64.tar.gz' --pattern 'agctl_linux_arm64.tar.gz' --pattern checksums.txt
```

Сверить SHA-256 скачанных архивов с опубликованными (должно быть `OK`):

```bash
shasum -a 256 -c checksums.txt --ignore-missing
```

Распаковать программу в `~/.local/bin` и снять метку «скачано из интернета», иначе Gatekeeper не даст её запустить:

```bash
tar xzf agctl_darwin_arm64.tar.gz agctl && mv agctl ~/.local/bin/agctl && xattr -d com.apple.quarantine ~/.local/bin/agctl 2>/dev/null; true
```

Положить linux-сборку в `~/.local/lib/agctl` под именем `agctl-linux-arm64`:

```bash
tar xzf agctl_linux_arm64.tar.gz agctl && mv agctl ~/.local/lib/agctl/agctl-linux-arm64
```

Проверить, что программа запускается и какой версии (`~/.local/bin` должен быть в `PATH`):

```bash
agctl version
```

Секреты уходят в Связку ключей macOS; при первом обращении система спросит
разрешение — «Разрешить всегда».

#### Linux (Docker Engine; `amd64` или `arm64`)

Создать каталог для программы и перейти во временную папку для скачивания:

```bash
mkdir -p ~/.local/bin && cd "$(mktemp -d)"
```

Скачать архив для Linux и файл контрольных сумм:

```bash
gh release download --repo arijjia/agctl --pattern 'agctl_linux_amd64.tar.gz' --pattern checksums.txt
```

Сверить SHA-256 архива с опубликованной (должно быть `OK`):

```bash
sha256sum -c checksums.txt --ignore-missing
```

Распаковать программу в `~/.local/bin`:

```bash
tar xzf agctl_linux_amd64.tar.gz agctl && mv agctl ~/.local/bin/agctl
```

Проверить, что программа запускается и какой версии (`~/.local/bin` должен быть в `PATH`):

```bash
agctl version
```

Сам бинарь и есть linux-сборка для образа, отдельно ничего класть не
нужно. Пользователь должен иметь доступ к Docker (группа `docker`). Без
Secret Service (сервер, WSL) секреты автоматически уходят в
`~/.agctl-dev/secrets.enc`. Мост к хосту для контейнеров слушает адрес
`docker0` сам.

</details>

### Вариант 2: собрать из исходников

Нужны **Go 1.27+**, **Node 22+** (сборка SPA) и Docker.

Склонировать репозиторий и перейти в него:

```bash
git clone https://github.com/arijjia/agctl && cd agctl
```

Собрать SPA, бинарь и linux-сборку и поставить их в `~/.local`:

```bash
make install
```

`make install` собирает SPA, бинарь и linux-сборку и ставит их в
`~/.local/bin/agctl`, `~/.local/lib/agctl/agctl-linux-<arch>` и
автодополнение fish в `~/.config/fish/completions/agctl.fish`
(`INSTALL_DIR=…` меняет каталог). Повторяйте после каждого `git pull`:
старый бинарь в `PATH` — самая частая причина ошибки
«неизвестная команда "dev"». `agctl version` печатает коммит и дату сборки.

Другие цели: `make go_build` — только бинарь в `dist/agctl` (без Node:
`web/dist` закоммичен); `make release_local` — матрица darwin / linux /
windows × arm64 / amd64 в `dist/` для другой машины; `make img_build` —
собрать образы рабочего места локально (`PROFILE=<id>` — с надстройкой
профиля организации из `profiles/<id>`); `make uninstall` — убрать то, что
поставил `install`. Полный список — `make help`.

## Запуск

Настроить: профиль организации и папка проектов (пишет `~/.agctl-dev/dev.yaml`):

```bash
agctl dev init
```

Поднять платформу в фоне и открыть браузер со ссылкой входа:

```bash
agctl dev up
```

`up` проверяет Docker и порт, запускает платформу на
`http://127.0.0.1:8930` и открывает браузер с одноразовой ссылкой входа. Пароля и регистрации нет: вы единственный пользователь с ролью
developer.

После входа открывается **лаунчер**:

- **Доступы** — ключ модели, адрес и токен GitLab, адрес, логин и токен
  Jira, свои переменные. Значения уходят в хранилище ОС (Keychain, Диспетчер
  учётных данных, Secret Service) и обратно на страницу не возвращаются.
  Пока ключа модели нет, лаунчер открывается на этой вкладке.
- **Рабочее место** — образ из профиля (базовый, `web` с Playwright и
  Chromium, вкус организации вроде `1c`) с отметкой «есть на машине».
  «Запустить» скачивает недостающий образ из реестра или собирает его здесь
  же (10–20 минут, прогресс на странице), затем поднимает контейнер.
- **Открыть чат** — dsh. Если образ на машине и ключ задан, рабочее место
  стартует сразу при входе.

Папка проектов становится `/workspace` в контейнере: это ваши настоящие
репозитории, автокоммитов нет, удалить папку из интерфейса нельзя.
`agctl dev up ~/src/проект` поднимает платформу с другой папкой.

Профиль организации (бренд, модели, образы, белый список команд хоста) —
каталог с `profile.yaml`: `agctl dev init --profile-dir <каталог>`. Без него
работает встроенный нейтральный профиль `agctl`.

## Команды бинарника для локального режима

| Команда | Что делает |
|---|---|
| `agctl dev init [--profile ID \| --profile-dir DIR] [--flavor web\|1c] [--image REF] [--workdir DIR] [--addr 127.0.0.1:PORT] [--yes] [--cli]` | настроить: профиль и папка проектов; образ и доступы — в лаунчере (`--cli` — мастером в терминале) |
| `agctl dev up [DIR] [--no-open] [--foreground]` | поднять платформу в фоне, открыть браузер; `DIR` → `/workspace` |
| `agctl dev down` | остановить платформу и контейнер; сессии и настройки сохраняются |
| `agctl dev status` | адрес, образ, папка, состояние рабочего места и моста |
| `agctl dev open` | новая ссылка входа в браузере |
| `agctl dev logs [-f]` | лог фонового процесса |
| `agctl dev secrets list \| set ИМЯ [--stdin] [--plain] \| rm ИМЯ` | доступы: список без значений, задать (ввод скрыт; `--plain` — несекретное в `dev.yaml`), удалить |
| `agctl dev bridge list \| pending \| approve ID \| deny ID \| log` | мост к хосту: белый список, вызовы, ждущие подтверждения, решение, журнал |
| `agctl dev update [--image REF]` | обновить образ: скачать → бэкап dsh-home → перезапуск → проверка → автооткат при сбое |
| `agctl dev rollback [--restore-data]` | вернуть предыдущий образ, при необходимости и данные dsh из бэкапа |
| `agctl dev doctor` | самодиагностика: Docker, образ, профиль, папка, доступы, сеть до модели / GitLab / Jira, мост |
| `agctl bridge list \| call ИНСТРУМЕНТ k=v … \| mcp` | **внутри контейнера**: вызвать команду хоста из белого списка; `mcp` — stdio-сервер MCP «Мост к хосту» |
| `agctl version` | версия, коммит, дата сборки, ОС и архитектура |
| `agctl completion fish` | скрипт автодополнения для fish |
| `agctl help` | справка по всем командам, `agctl dev help` — по локальному режиму |

Переменные окружения: `AGCTL_DEV_HOME` — дом режима (по умолчанию
`~/.agctl-dev`), `AGCTL_DEV_SECRETS=file` — хранить секреты в
`~/.agctl-dev/secrets.enc` вместо хранилища ОС, `NO_COLOR` — справка без
цвета.

Дом режима `~/.agctl-dev`: `dev.yaml` (настройки без секретов), `data/`
(SQLite и dsh-home: сессии, настройки dsh), `run/state.json` (фоновый
процесс), `logs/` (`serve.log`, `bridge.log`), `backups/` (три последних
бэкапа dsh-home). Серверный `~/.agctl` и `~/.config/agctl/config.yaml` на
локальный режим не влияют.

### Мост к хосту

Агент работает в Linux-контейнере, а часть программ есть только на вашей
машине (например, платформа 1С на Windows). Мост даёт агенту **только**
команды из белого списка `bridge.tools` в `dev.yaml`; профиль организации
может положить список по умолчанию. Команда запускается без shell,
аргументы проверяются, инструменты с `confirm: true` ждут вашего
`agctl dev bridge approve ID`, каждый вызов пишется в журнал.

### Если что-то не так

| Симптом | Что сделать |
|---|---|
| `неизвестная команда "dev"` | в `PATH` старый бинарь: `agctl version`, затем `make install` или свежий релиз |
| `docker недоступен` | запустите Docker Desktop / dockerd |
| `образ … нет локально` | `docker login <реестр>` и `agctl dev update`, либо «Запустить» в лаунчере соберёт образ на месте |
| «Добавьте ключ API» в чате | вкладка «Доступы» или `agctl dev secrets set AGCTL_LLM_API_KEY` |
| `адрес 127.0.0.1:8930 занят` | `agctl dev down`; порт меняется ключом `addr` в `dev.yaml` |
| «не удаётся проверить разработчика» (macOS) | `xattr -d com.apple.quarantine` на бинаре |
| что-то ещё | `agctl dev doctor`, `agctl dev logs -f`, `agctl version` |

## Карта системы

Открыть карту в браузере (из клона репозитория):

```bash
make map
```

Открывает `system/map/index.html` с тремя вкладками: «Сервер — один хост»,
«Ноутбук — локальный режим», «Кластер: k8s | ax». Карты собираются archify
из спецификаций `system/map/*.architecture.json`, улики ведут в строки кода;
`make map_check` проверяет их против текущего дерева, `make map_build`
пересобирает HTML (нужен archify, `ARCHIFY=…`). На работающем сервере те
же вкладки — страница `/map` за сессией.

## Документация

- [docs/local-mode-architecture.md](docs/local-mode-architecture.md) —
  локальный режим: архитектура, реализация, какая часть кодовой базы за
  что отвечает (локальный и удалённый режимы), разбор репозитория.
- Прежние документы (серверная архитектура v3, Kubernetes, кастомизация
  профилем, безопасность шлюза, capacity) переехали в архив штаба:
  `agctl-hq/docs/`.
- Карточки задач — `backlog/open/`; правила для агентов — `AGENTS.md`.

## Лицензия

[MIT](LICENSE)
