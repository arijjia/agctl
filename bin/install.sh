#!/bin/sh
# agctl — установка бинаря из GitHub Releases одной командой (macOS, Linux):
#
#   curl -fsSL https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.sh | sh
#
# Что делает: определяет ОС и архитектуру, скачивает архив релиза и
# checksums.txt (через gh, если он есть и авторизован, иначе curl), сверяет
# SHA-256, кладёт бинарь в ~/.local/bin, снимает карантин macOS, на macOS
# дополнительно кладёт linux-сборку в ~/.local/lib/agctl (нужна только для
# самосборки образа рабочего места без реестра), печатает agctl version.
#
# Переменные окружения:
#   AGCTL_VERSION      тег релиза (по умолчанию latest)
#   AGCTL_INSTALL_DIR  куда ставить бинарь (по умолчанию ~/.local/bin)
#   AGCTL_LIB_DIR      куда класть linux-сборку (по умолчанию <INSTALL_DIR>/../lib/agctl)
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить linux-сборку
#
# Windows — bin/install.ps1.
set -eu

REPO="${AGCTL_REPO:-arijjia/agctl}"
VERSION="${AGCTL_VERSION:-latest}"
INSTALL_DIR="${AGCTL_INSTALL_DIR:-$HOME/.local/bin}"
LIB_DIR="${AGCTL_LIB_DIR:-$INSTALL_DIR/../lib/agctl}"

say() { printf '%s\n' "$*"; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin | linux) ;;
  *) die "ОС $os не поддерживается (darwin, linux); для Windows — bin/install.ps1" ;;
esac
case "$(uname -m)" in
  arm64 | aarch64) arch=arm64 ;;
  x86_64 | amd64) arch=amd64 ;;
  *) die "архитектура $(uname -m) не поддерживается (arm64, amd64)" ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
cd "$tmp"

# fetch <файл>: из AGCTL_RELEASE_DIR, через gh (приватные релизы) или curl.
fetch() {
  if [ -n "${AGCTL_RELEASE_DIR:-}" ]; then
    cp "$AGCTL_RELEASE_DIR/$1" . || die "в $AGCTL_RELEASE_DIR нет $1"
    return
  fi
  if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
    if [ "$VERSION" = latest ]; then
      gh release download --repo "$REPO" --pattern "$1" --clobber || die "gh release download $1: релизов ещё нет?"
    else
      gh release download "$VERSION" --repo "$REPO" --pattern "$1" --clobber || die "gh release download $VERSION $1"
    fi
    return
  fi
  command -v curl >/dev/null 2>&1 || die "нужен curl или авторизованный gh"
  if [ "$VERSION" = latest ]; then
    url="https://github.com/$REPO/releases/latest/download/$1"
  else
    url="https://github.com/$REPO/releases/download/$VERSION/$1"
  fi
  curl -fsSL -o "$1" "$url" || die "не удалось скачать $url (релизов ещё нет? приватный репозиторий — gh auth login)"
}

# verify <файл>: SHA-256 против checksums.txt.
verify() {
  line=$(grep -F " $1" checksums.txt | head -1)
  [ -n "$line" ] || die "в checksums.txt нет записи для $1"
  want=${line%% *}
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$1" | cut -d' ' -f1)
  else
    got=$(shasum -a 256 "$1" | cut -d' ' -f1)
  fi
  [ "$want" = "$got" ] || die "контрольная сумма $1 не совпала с checksums.txt"
}

main_archive="agctl_${os}_${arch}.tar.gz"
linux_archive="agctl_linux_${arch}.tar.gz"
say "agctl: $os/$arch · релиз $VERSION · каталог $INSTALL_DIR"

fetch checksums.txt
fetch "$main_archive"
verify "$main_archive"
tar xzf "$main_archive" agctl
mkdir -p "$INSTALL_DIR"
install -m 0755 agctl "$INSTALL_DIR/agctl"
if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$INSTALL_DIR/agctl" 2>/dev/null || true
fi
say "✓ бинарь: $INSTALL_DIR/agctl"

# На Linux сам бинарь и есть linux-сборка для образа; на macOS она нужна отдельно.
if [ "$os" = darwin ] && [ -z "${AGCTL_NO_LINUX:-}" ]; then
  fetch "$linux_archive"
  verify "$linux_archive"
  rm -f agctl
  tar xzf "$linux_archive" agctl
  mkdir -p "$LIB_DIR"
  LIB_DIR=$(cd "$LIB_DIR" && pwd)
  install -m 0755 agctl "$LIB_DIR/agctl-linux-$arch"
  say "✓ linux-сборка для самосборки образа: $LIB_DIR/agctl-linux-$arch"
fi

say "✓ $("$INSTALL_DIR/agctl" version)"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say "ℹ $INSTALL_DIR нет в PATH — добавьте в профиль оболочки, например: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
say "Дальше: agctl dev init, затем agctl dev up"
