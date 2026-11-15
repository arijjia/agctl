# Установка agctl вручную, по командам

Обычно достаточно установки одной командой («Вариант 1» в
[README](../README.md)) — скрипт делает все шаги ниже сам. Здесь те же шаги
руками: если скрипт запускать нельзя или хочется видеть, что именно он
делает.

## Windows 10 / 11 (PowerShell, Docker Desktop)

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

## macOS (Apple Silicon; для Intel замените `arm64` на `amd64`)

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

## Linux (Docker Engine; `amd64` или `arm64`)

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

## Подключить профиль комплекта организации (обычно делает скрипт)

При установке комплекта (`AGCTL_REPO=<org>/agctl-profile-<id>`) скрипт сам
выполняет `agctl dev init --profile-dir <каталог> --yes`. Вручную (или при
`AGCTL_NO_INIT=1`) те же два шага:

```bash
agctl dev init --profile-dir ~/.local/share/agctl/profiles/<id> --yes   # Windows: --profile-dir "%LOCALAPPDATA%\agctl\profiles\<id>"
agctl dev up
```

`--yes` — не задавать вопросов (папка проектов по умолчанию `~/src`,
передайте свою: `--workdir C:\src\проекты`). Доступы (ключ модели, GitLab,
Jira) при этом не спрашиваются — их спросит лаунчер в браузере при первом
`dev up` и положит в хранилище ОС.
