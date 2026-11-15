# agctl — установка бинаря из GitHub Releases одной командой (Windows, PowerShell 5.1+):
#
#   irm https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.ps1 | iex
#
# Что делает: скачивает архив релиза и checksums.txt (через gh, если он есть и
# авторизован, иначе Invoke-WebRequest), сверяет SHA-256, кладёт agctl.exe в
# %LOCALAPPDATA%\agctl, снимает отметку «скачано из интернета», рядом кладёт
# linux-сборку (нужна только для самосборки образа рабочего места без
# реестра), добавляет каталог в PATH пользователя, печатает agctl version.
#
# Переменные окружения:
#   AGCTL_VERSION      тег релиза (по умолчанию latest)
#   AGCTL_INSTALL_DIR  куда ставить (по умолчанию %LOCALAPPDATA%\agctl)
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить linux-сборку
#
# macOS и Linux — bin/install.sh.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$repo = if ($env:AGCTL_REPO) { $env:AGCTL_REPO } else { 'arijjia/agctl' }
$version = if ($env:AGCTL_VERSION) { $env:AGCTL_VERSION } else { 'latest' }
$installDir = if ($env:AGCTL_INSTALL_DIR) { $env:AGCTL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'agctl' }
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$mainArchive = "agctl_windows_$arch.zip"
$linuxArchive = "agctl_linux_$arch.tar.gz"

# Native <exe> <args…>: запуск внешней программы без превращения stderr в
# исключение (PowerShell 5.1); возвращает код выхода.
function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { & $Exe @Arguments 2>&1 | Out-Null; return $LASTEXITCODE } finally { $ErrorActionPreference = $prev }
}

function Fetch([string]$Name) {
    if ($env:AGCTL_RELEASE_DIR) {
        Copy-Item (Join-Path $env:AGCTL_RELEASE_DIR $Name) . -Force
        return
    }
    if ((Get-Command gh -ErrorAction SilentlyContinue) -and (Invoke-Native 'gh' @('auth', 'status')) -eq 0) {
        $ghArgs = if ($version -eq 'latest') { @('release', 'download', '--repo', $repo, '--pattern', $Name, '--clobber') }
                  else { @('release', 'download', $version, '--repo', $repo, '--pattern', $Name, '--clobber') }
        if ((Invoke-Native 'gh' $ghArgs) -ne 0) { throw "gh release download $Name завершился с ошибкой (релизов ещё нет?)" }
        return
    }
    $url = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download/$Name" }
           else { "https://github.com/$repo/releases/download/$version/$Name" }
    try { Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $Name }
    catch { throw "не удалось скачать $url (релизов ещё нет? приватный репозиторий — gh auth login): $($_.Exception.Message)" }
}

function Verify([string]$Name) {
    $line = Get-Content checksums.txt | Where-Object { $_ -match ('\s' + [regex]::Escape($Name) + '$') } | Select-Object -First 1
    if (-not $line) { throw "в checksums.txt нет записи для $Name" }
    $want = ($line -split '\s+')[0].ToLower()
    $got = (Get-FileHash $Name -Algorithm SHA256).Hash.ToLower()
    if ($want -ne $got) { throw "контрольная сумма $Name не совпала с checksums.txt" }
}

$tmp = Join-Path $env:TEMP ('agctl-install-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $tmp | Out-Null
Push-Location $tmp
try {
    Write-Host "agctl: windows/$arch · релиз $version · каталог $installDir"
    Fetch 'checksums.txt'
    Fetch $mainArchive
    Verify $mainArchive
    Expand-Archive -Force $mainArchive -DestinationPath .
    New-Item -ItemType Directory -Force $installDir | Out-Null
    $exe = Join-Path $installDir 'agctl.exe'
    Move-Item -Force agctl.exe $exe
    Unblock-File $exe
    Write-Host "✓ бинарь: $exe"

    if (-not $env:AGCTL_NO_LINUX) {
        if (Get-Command tar -ErrorAction SilentlyContinue) {
            Fetch $linuxArchive
            Verify $linuxArchive
            if ((Invoke-Native 'tar' @('xzf', $linuxArchive, 'agctl')) -ne 0) { throw "tar не распаковал $linuxArchive" }
            $linux = Join-Path $installDir "agctl-linux-$arch"
            Move-Item -Force agctl $linux
            Write-Host "✓ linux-сборка для самосборки образа: $linux"
        } else {
            Write-Warning 'tar не найден (Windows 10 до 1803) — linux-сборка для самосборки образа пропущена'
        }
    }

    $userPath = [string][Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $installDir) {
        [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $installDir).TrimStart(';'), 'User')
        Write-Host "✓ $installDir добавлен в PATH пользователя (новые окна PowerShell увидят его сами)"
    }
    if (($env:Path -split ';') -notcontains $installDir) { $env:Path += ";$installDir" }

    Write-Host ('✓ ' + ((& $exe version) -join ' '))
    Write-Host 'Дальше: agctl dev init, затем agctl dev up'
} finally {
    Pop-Location
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
