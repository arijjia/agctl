# agctl — установка бинаря одной командой (Windows, PowerShell 5.1+):
#
#   irm https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.ps1 | iex
#
# Два источника:
#   • публичный релиз agctl (goreleaser): agctl_windows_<arch>.zip,
#     agctl-guest_linux_<arch>.tar.gz (локальный guest),
#     agctl-guest-server_linux_<arch>.tar.gz (серверный guest) + checksums.txt;
#   • комплект организации — релиз приватного репозитория профиля
#     $env:AGCTL_REPO = '<org>/agctl-profile-<id>': zip agctl-<id>-<версия>-windows-<arch>.zip
#     (agctl.exe + agctl-guest-linux-<arch> + agctl-guest-server-linux-<arch> + profile\) +
#     checksums-<версия>.txt; нужен авторизованный gh.
#
# Всё ставится под один корень — %USERPROFILE%\.agctl (переменная AGCTL_ROOT);
# раскладка та же, что на macOS/Linux (internal/localmode/layout.go):
#
#   <корень>\bin\agctl.exe                            бинарь (этот каталог — в PATH)
#   <корень>\bin\guest\agctl-guest-linux-<arch>       guest локального образа agctl-dsh*
#   <корень>\bin\guest\agctl-guest-server-linux-<arch> guest серверного образа
#   <корень>\profiles\<id>\                           профиль комплекта организации
#   <корень>\homes\<id>\                              дом (создаёт agctl dev init)
#
# Что делает: скачивает архив и контрольные суммы (через gh, если он есть и
# авторизован, иначе Invoke-WebRequest), сверяет SHA-256, кладёт agctl.exe и
# оба agctl-guest (нужны только для самосборки образа без реестра), снимает
# отметку «скачано из интернета», добавляет <корень>\bin в PATH пользователя,
# печатает agctl version. Комплект организации: профиль кладётся в
# <корень>\profiles\<id>, и скрипт сам создаёт для него СВОЙ дом homes\<id>
# (agctl dev --home <id> init --profile <id> --yes) — дом по умолчанию и
# другие дома не трогаются; AGCTL_NO_INIT=1 — не создавать, напечатать команду.
#
# Переменные окружения:
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl;
#                      <org>/agctl-profile-<id> — комплект организации)
#   AGCTL_VERSION      тег релиза (по умолчанию последний)
#   AGCTL_ROOT         корень установки (по умолчанию %USERPROFILE%\.agctl)
#   AGCTL_WORKDIR      папка проектов для авто-init комплекта (по умолчанию ~\src)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить agctl-guest (образы берутся из реестра)
#   AGCTL_NO_INIT=1    не создавать дом комплекта самим — напечатать команду
#
# macOS и Linux — bin/install.sh.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = if ($env:AGCTL_REPO) { $env:AGCTL_REPO } else { 'arijjia/agctl' }
$version = $env:AGCTL_VERSION
$root = if ($env:AGCTL_ROOT) { $env:AGCTL_ROOT } else { Join-Path $env:USERPROFILE '.agctl' }
$env:AGCTL_ROOT = $root   # agctl ниже работает с этим же корнем
$installDir = Join-Path $root 'bin'
$guestDir = Join-Path $installDir 'guest' 
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$repoName = $repo.Split('/')[-1]
$profileId = if ($repoName -like 'agctl-profile-?*') { $repoName.Substring('agctl-profile-'.Length) } else { '' }
$profileDir = Join-Path (Join-Path $root 'profiles') $profileId

# Invoke-Native: внешняя программа без превращения stderr в исключение
# (PowerShell 5.1); возвращает код выхода, вывод — в $script:nativeOut.
function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $script:nativeOut = (& $Exe @Arguments 2>&1 | ForEach-Object { "$_" }) -join "`n"
        return $LASTEXITCODE
    } finally { $ErrorActionPreference = $prev }
}

$ghOk = $false
if (Get-Command gh -ErrorAction SilentlyContinue) { $ghOk = ((Invoke-Native 'gh' @('auth', 'status')) -eq 0) }

function Fetch([string]$Name) {
    if ($env:AGCTL_RELEASE_DIR) {
        Copy-Item (Join-Path $env:AGCTL_RELEASE_DIR $Name) . -Force
        return
    }
    if ($ghOk) {
        if ((Invoke-Native 'gh' @('release', 'download', $version, '--repo', $repo, '--pattern', $Name, '--clobber')) -ne 0) {
            throw "gh release download $version $Name из $repo: $script:nativeOut"
        }
        return
    }
    $url = "https://github.com/$repo/releases/download/$version/$Name"
    try { Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $Name }
    catch { throw "не удалось скачать $url (приватный репозиторий или релизов ещё нет — gh auth login): $($_.Exception.Message)" }
}

# Get-LatestTag: последний релиз репозитория (включая pre-release).
function Get-LatestTag {
    if ($env:AGCTL_RELEASE_DIR) {
        $f = Get-ChildItem (Join-Path $env:AGCTL_RELEASE_DIR 'checksums-*.txt') -ErrorAction SilentlyContinue | Sort-Object Name | Select-Object -Last 1
        if ($f) { return 'v' + $f.BaseName.Substring('checksums-'.Length) }
        return 'latest'
    }
    if ($ghOk) {
        if ((Invoke-Native 'gh' @('release', 'list', '--repo', $repo, '--limit', '1', '--json', 'tagName', '--jq', '.[0].tagName')) -ne 0) { return '' }
        return $script:nativeOut.Trim()
    }
    try {
        $r = Invoke-RestMethod -UseBasicParsing -Uri "https://api.github.com/repos/$repo/releases/latest"
        return $r.tag_name
    } catch { return '' }
}

function Verify([string]$Name, [string]$Sums) {
    $line = Get-Content $Sums | Where-Object { $_ -match ('\s' + [regex]::Escape($Name) + '$') } | Select-Object -First 1
    if (-not $line) { throw "в $Sums нет записи для $Name" }
    $want = ($line -split '\s+')[0].ToLower()
    $got = (Get-FileHash $Name -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) { throw "контрольная сумма $Name не совпала с $Sums" }
}

function Install-LinuxBuild([string]$Source, [string]$Name) {
    New-Item -ItemType Directory -Force $guestDir | Out-Null
    $linux = Join-Path $guestDir $Name
    Move-Item -Force $Source $linux
    Write-Host "✓ бинарь образа для самосборки: $linux"
}

$tmp = Join-Path $env:TEMP ('agctl-install-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $tmp | Out-Null
Push-Location $tmp
try {
    if (-not $version) {
        $version = Get-LatestTag
        if (-not $version) { throw "не удалось определить последний релиз $repo (релизов ещё нет?)" }
    }
    $ver = $version -replace '^v', ''
    Write-Host "agctl: windows/$arch · $repo · релиз $version · корень $root"
    New-Item -ItemType Directory -Force $installDir | Out-Null
    $exe = Join-Path $installDir 'agctl.exe'

    if ($profileId) {
        # ── комплект организации ────────────────────────────────────────────
        $bundle = "agctl-$profileId-$ver-windows-$arch.zip"
        $sums = "checksums-$ver.txt"
        Fetch $sums
        Fetch $bundle
        Verify $bundle $sums
        Expand-Archive -Force $bundle -DestinationPath .
        $top = $bundle -replace '\.zip$', ''
        if (-not (Test-Path (Join-Path $top 'agctl.exe'))) { throw "в $bundle нет $top\agctl.exe" }
        Move-Item -Force (Join-Path $top 'agctl.exe') $exe
        if (-not $env:AGCTL_NO_LINUX) {
            $guest = "agctl-guest-linux-$arch"
            if (Test-Path (Join-Path $top $guest)) {
                Install-LinuxBuild (Join-Path $top $guest) $guest
            } else {
                Write-Host "ℹ в комплекте нет $guest — образ рабочего места только из реестра"
            }
            $guestServer = "agctl-guest-server-linux-$arch"
            if (Test-Path (Join-Path $top $guestServer)) {
                Install-LinuxBuild (Join-Path $top $guestServer) $guestServer
            }
        }
        if (-not (Test-Path (Join-Path $top 'profile\profile.yaml'))) { throw 'в комплекте нет profile\profile.yaml' }
        New-Item -ItemType Directory -Force (Split-Path $profileDir -Parent) | Out-Null
        $existing = Get-Item $profileDir -Force -ErrorAction SilentlyContinue
        if ($existing -and $existing.LinkType) {
            # Работа из исходников: profiles\<id> — ссылка на репозиторий профиля.
            Write-Host "ℹ $profileDir — ссылка; профиль комплекта не распакован, ссылка оставлена"
        } else {
            if ($existing) { Remove-Item -Recurse -Force $profileDir }
            Move-Item -Force (Join-Path $top 'profile') $profileDir
            Write-Host "✓ профиль ${profileId}: $profileDir"
        }
    } else {
        # ── публичный релиз goreleaser ──────────────────────────────────────
        $mainArchive = "agctl_windows_$arch.zip"
        Fetch 'checksums.txt'
        Fetch $mainArchive
        Verify $mainArchive 'checksums.txt'
        Expand-Archive -Force $mainArchive -DestinationPath .
        Move-Item -Force agctl.exe $exe
        if (-not $env:AGCTL_NO_LINUX) {
            if (Get-Command tar -ErrorAction SilentlyContinue) {
                $guestArchive = "agctl-guest_linux_$arch.tar.gz"
                Fetch $guestArchive
                Verify $guestArchive 'checksums.txt'
                if ((Invoke-Native 'tar' @('xzf', $guestArchive, 'agctl-guest')) -ne 0) { throw "tar не распаковал $guestArchive" }
                Install-LinuxBuild 'agctl-guest' "agctl-guest-linux-$arch"
                $serverArchive = "agctl-guest-server_linux_$arch.tar.gz"
                Fetch $serverArchive
                Verify $serverArchive 'checksums.txt'
                if ((Invoke-Native 'tar' @('xzf', $serverArchive, 'agctl-guest')) -ne 0) { throw "tar не распаковал $serverArchive" }
                Install-LinuxBuild 'agctl-guest' "agctl-guest-server-linux-$arch"
            } else {
                Write-Warning 'tar не найден (Windows 10 до 1803) — бинарь образа для самосборки пропущен'
            }
        }
    }

    Unblock-File $exe
    Write-Host "✓ бинарь: $exe"

    $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $installDir) {
        [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $installDir).TrimStart(';'), 'User')
        Write-Host "✓ $installDir добавлен в PATH пользователя (новые окна PowerShell увидят его сами)"
    }
    if (($env:Path -split ';') -notcontains $installDir) { $env:Path += ";$installDir" }

    Write-Host ('✓ ' + ((& $exe version) -join ' '))
    if ($profileId -and -not $env:AGCTL_NO_INIT) {
        # Свой дом комплекта — homes\<id>; дом по умолчанию и чужие дома не
        # трогаются (--yes — не спрашивать; доступы спросит лаунчер).
        $initArgs = @('dev', '--home', $profileId, 'init', '--profile', $profileId, '--yes')
        if ($env:AGCTL_WORKDIR) { $initArgs += @('--workdir', $env:AGCTL_WORKDIR) }
        & $exe @initArgs
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "agctl dev init не удался — создайте дом сами: agctl dev --home $profileId init --profile $profileId"
        }
        Write-Host "Дальше: agctl dev --home $profileId up   (где что лежит — agctl dev --home $profileId paths)"
    } elseif ($profileId) {
        Write-Host "Дальше: agctl dev --home $profileId init --profile $profileId [--flavor …] [--workdir C:\src\...], затем agctl dev --home $profileId up"
    } else {
        Write-Host 'Дальше: agctl dev up — первый запуск настроит дом сам (папка проектов ~/src); тонкая настройка — agctl dev init; где что лежит — agctl dev paths'
    }
    if (Get-ChildItem -Force -Directory $env:USERPROFILE -Filter '.agctl-dev*' -ErrorAction SilentlyContinue) {
        Write-Host "ℹ найдены прежние дома ~\.agctl-dev* — перенос в $root\homes: agctl dev migrate (план), agctl dev migrate --apply"
    }
} finally {
    Pop-Location
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
