# agctl — установка бинаря одной командой (Windows, PowerShell 5.1+):
#
#   irm https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.ps1 | iex
#
# Два источника:
#   • публичный релиз agctl (goreleaser): agctl_windows_<arch>.zip + checksums.txt;
#   • комплект организации — релиз приватного репозитория профиля
#     $env:AGCTL_REPO = '<org>/agctl-profile-<id>': zip agctl-<id>-<версия>-windows-<arch>.zip
#     (agctl.exe + agctl-linux-<arch> + profile\) + checksums-<версия>.txt; нужен
#     авторизованный gh. Профиль кладётся в %LOCALAPPDATA%\agctl\profiles\<id>,
#     в конце печатается команда agctl dev init.
#
# Что делает: скачивает архив и контрольные суммы (через gh, если он есть и
# авторизован, иначе Invoke-WebRequest), сверяет SHA-256, кладёт agctl.exe в
# %LOCALAPPDATA%\agctl, снимает отметку «скачано из интернета», рядом кладёт
# linux-сборку (нужна только для самосборки образа рабочего места без
# реестра), добавляет каталог в PATH пользователя, печатает agctl version.
#
# Переменные окружения:
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl;
#                      <org>/agctl-profile-<id> — комплект организации)
#   AGCTL_VERSION      тег релиза (по умолчанию последний)
#   AGCTL_INSTALL_DIR  куда ставить (по умолчанию %LOCALAPPDATA%\agctl)
#   AGCTL_PROFILE_DIR  куда класть профиль комплекта (по умолчанию <INSTALL_DIR>\profiles\<id>)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить linux-сборку
#
# macOS и Linux — bin/install.sh.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = if ($env:AGCTL_REPO) { $env:AGCTL_REPO } else { 'arijjia/agctl' }
$version = $env:AGCTL_VERSION
$installDir = if ($env:AGCTL_INSTALL_DIR) { $env:AGCTL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'agctl' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$repoName = $repo.Split('/')[-1]
$profileId = if ($repoName -like 'agctl-profile-?*') { $repoName.Substring('agctl-profile-'.Length) } else { '' }
$profileDir = if ($env:AGCTL_PROFILE_DIR) { $env:AGCTL_PROFILE_DIR } else { Join-Path (Join-Path $installDir 'profiles') $profileId }

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

function Install-LinuxBuild([string]$Source) {
    $linux = Join-Path $installDir "agctl-linux-$arch"
    Move-Item -Force $Source $linux
    Write-Host "✓ linux-сборка для самосборки образа: $linux"
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
    Write-Host "agctl: windows/$arch · $repo · релиз $version · каталог $installDir"
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
        if (-not $env:AGCTL_NO_LINUX) { Install-LinuxBuild (Join-Path $top "agctl-linux-$arch") }
        if (-not (Test-Path (Join-Path $top 'profile\profile.yaml'))) { throw 'в комплекте нет profile\profile.yaml' }
        New-Item -ItemType Directory -Force (Split-Path $profileDir -Parent) | Out-Null
        if (Test-Path $profileDir) { Remove-Item -Recurse -Force $profileDir }
        Move-Item -Force (Join-Path $top 'profile') $profileDir
        Write-Host "✓ профиль ${profileId}: $profileDir"
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
                $linuxArchive = "agctl_linux_$arch.tar.gz"
                Fetch $linuxArchive
                Verify $linuxArchive 'checksums.txt'
                if ((Invoke-Native 'tar' @('xzf', $linuxArchive, 'agctl')) -ne 0) { throw "tar не распаковал $linuxArchive" }
                Install-LinuxBuild 'agctl'
            } else {
                Write-Warning 'tar не найден (Windows 10 до 1803) — linux-сборка для самосборки образа пропущена'
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
    if ($profileId) {
        Write-Host "Дальше: agctl dev init --profile-dir `"$profileDir`" [--flavor …] [--workdir C:\src\...], затем agctl dev up"
    } else {
        Write-Host 'Дальше: agctl dev init, затем agctl dev up'
    }
} finally {
    Pop-Location
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
