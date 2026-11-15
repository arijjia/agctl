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

- **Docker**: Docker Desktop (macOS, Windows с WSL2), Docker Engine (Linux),
  либо Colima / Rancher Desktop / Podman Desktop. `docker version` должен
  отвечать без ошибок.
- **Бинарь `agctl`** под вашу ОС — ниже два способа его получить.
- 8 ГБ ОЗУ и ~15 ГБ диска: образ рабочего места с браузерами занимает
  2–3 ГБ.
- Ключ модели (OpenAI-совместимый провайдер: LiteLLM организации или
  публичный) — выдаёт команда платформы.

## Получить бинарь

### Вариант 1: скачать с GitHub

Релизы собираются goreleaser по тегу `v*` и публикуются на странице
[Releases](https://github.com/arijjia/agctl/releases): архивы
`agctl_<ос>_<архитектура>.tar.gz` (для Windows — `.zip`) и `checksums.txt`.
Если список релизов пуст, тег ещё не выпускался: соберите из исходников
(вариант 2) или возьмите сборку у команды платформы. Организации могут
публиковать и собственный релиз (бинарь + профиль организации в одном
архиве) в своём приватном репозитории.

macOS на Apple Silicon (для Intel замените `arm64` на `amd64`):

```bash
mkdir -p ~/.local/bin ~/.local/lib/agctl && cd "$(mktemp -d)"
```

```bash
gh release download --repo arijjia/agctl --pattern 'agctl_darwin_arm64.tar.gz' --pattern 'agctl_linux_arm64.tar.gz' --pattern checksums.txt
```

```bash
shasum -a 256 -c checksums.txt --ignore-missing
```

```bash
tar xzf agctl_darwin_arm64.tar.gz agctl && mv agctl ~/.local/bin/agctl && xattr -d com.apple.quarantine ~/.local/bin/agctl 2>/dev/null; true
```

```bash
tar xzf agctl_linux_arm64.tar.gz agctl && mv agctl ~/.local/lib/agctl/agctl-linux-arm64
```

```bash
agctl version
```

Что здесь что:

- `agctl_darwin_arm64.tar.gz` — сама программа; кладётся в `~/.local/bin`
  (каталог должен быть в `PATH`).
- `agctl_linux_arm64.tar.gz` — linux-сборка того же бинаря. Она нужна только
  если образ рабочего места придётся **собирать на вашей машине** (его нет
  в реестре организации): `agctl dev up` ищет `agctl-linux-<arch>` рядом с
  бинарём или в `../lib/agctl` относительно него. Если образы берутся из
  реестра, этот шаг можно пропустить.
- `xattr -d com.apple.quarantine` снимает метку «скачано из интернета»,
  иначе Gatekeeper не даст запустить неподписанную программу.
- Без `gh` то же самое делается через браузер: Releases → Assets.

Linux: архив `agctl_linux_<arch>.tar.gz`; сам бинарь и есть linux-сборка,
отдельно ничего класть не нужно.
Windows: `agctl_windows_<arch>.zip`, распаковать `agctl.exe` в каталог из
`PATH`; Docker Desktop с WSL2.

### Вариант 2: собрать из исходников

Нужны **Go 1.27+**, **Node 22+** (сборка SPA) и Docker.

```bash
git clone https://github.com/arijjia/agctl && cd agctl
```

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

```bash
agctl dev init
```

```bash
agctl dev up
```

`init` спрашивает профиль организации и папку проектов и пишет
`~/.agctl-dev/dev.yaml`. `up` проверяет Docker и порт, запускает платформу
в фоне на `http://127.0.0.1:8930` и открывает браузер с одноразовой ссылкой
входа. Пароля и регистрации нет: вы единственный пользователь с ролью
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
