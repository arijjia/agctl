#!/bin/sh
# agctl — установка бинаря одной командой (macOS, Linux):
#
#   curl -fsSL https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.sh | sh
#
# Два источника:
#   • публичный релиз agctl (goreleaser): архивы agctl_<os>_<arch>.tar.gz + checksums.txt;
#   • комплект организации — релиз приватного репозитория профиля
#     AGCTL_REPO=<org>/agctl-profile-<id>: zip agctl-<id>-<версия>-<os>-<arch>.zip
#     (agctl + agctl-linux-<arch> + profile/) + checksums-<версия>.txt; нужен
#     авторизованный gh и unzip. Профиль кладётся в
#     ~/.local/share/agctl/profiles/<id>, в конце печатается команда agctl dev init.
#
# Что делает: определяет ОС и архитектуру, скачивает архив и контрольные
# суммы (через gh, если он есть и авторизован, иначе curl), сверяет SHA-256,
# кладёт бинарь в ~/.local/bin, снимает карантин macOS, кладёт linux-сборку
# в ~/.local/lib/agctl (нужна только для самосборки образа рабочего места
# без реестра), печатает agctl version.
#
# Переменные окружения:
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl;
#                      <org>/agctl-profile-<id> — комплект организации)
#   AGCTL_VERSION      тег релиза (по умолчанию последний)
#   AGCTL_INSTALL_DIR  куда ставить бинарь (по умолчанию ~/.local/bin)
#   AGCTL_LIB_DIR      куда класть linux-сборку (по умолчанию <INSTALL_DIR>/../lib/agctl)
#   AGCTL_PROFILE_DIR  куда класть профиль комплекта (по умолчанию
#                      ${XDG_DATA_HOME:-~/.local/share}/agctl/profiles/<id>)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить linux-сборку
#
# Windows — bin/install.ps1.
set -eu

REPO="${AGCTL_REPO:-arijjia/agctl}"
VERSION="${AGCTL_VERSION:-}"
INSTALL_DIR="${AGCTL_INSTALL_DIR:-$HOME/.local/bin}"
LIB_DIR="${AGCTL_LIB_DIR:-$INSTALL_DIR/../lib/agctl}"

say() { printf '%s\n' "$*"; }
die() { printf 'install: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

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

# Комплект организации: репозиторий профиля <org>/agctl-profile-<id>.
profile_id=""
case "${REPO##*/}" in
  agctl-profile-?*) profile_id="${REPO##*/agctl-profile-}" ;;
esac
PROFILE_DIR="${AGCTL_PROFILE_DIR:-${XDG_DATA_HOME:-$HOME/.local/share}/agctl/profiles/$profile_id}"

gh_ok=0
if have gh && gh auth status >/dev/null 2>&1; then gh_ok=1; fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM
cd "$tmp"

# fetch <файл>: из AGCTL_RELEASE_DIR, через gh (приватные релизы) или curl.
fetch() {
  if [ -n "${AGCTL_RELEASE_DIR:-}" ]; then
    cp "$AGCTL_RELEASE_DIR/$1" . || die "в $AGCTL_RELEASE_DIR нет $1"
    return
  fi
  if [ "$gh_ok" = 1 ]; then
    gh release download "$VERSION" --repo "$REPO" --pattern "$1" --clobber || die "gh release download $VERSION $1 из $REPO"
    return
  fi
  have curl || die "нужен curl или авторизованный gh"
  url="https://github.com/$REPO/releases/download/$VERSION/$1"
  curl -fsSL -o "$1" "$url" || die "не удалось скачать $url (приватный репозиторий или релизов ещё нет — gh auth login)"
}

# latest_tag: последний релиз репозитория (включая pre-release).
latest_tag() {
  if [ -n "${AGCTL_RELEASE_DIR:-}" ]; then
    # по самому новому checksums-<версия>.txt в папке (комплект) или latest (goreleaser)
    f=$(ls "$AGCTL_RELEASE_DIR"/checksums-*.txt 2>/dev/null | sort | tail -1)
    if [ -n "$f" ]; then f=${f##*/checksums-}; printf 'v%s' "${f%.txt}"; else printf 'latest'; fi
    return
  fi
  if [ "$gh_ok" = 1 ]; then
    gh release list --repo "$REPO" --limit 1 --json tagName --jq '.[0].tagName'
    return
  fi
  have curl || die "нужен curl или авторизованный gh"
  # публичный репозиторий: releases/latest (pre-release сюда не попадают)
  curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1
}

# verify <файл> <checksums>: SHA-256 против файла контрольных сумм.
verify() {
  line=$(grep -F " $1" "$2" | head -1)
  [ -n "$line" ] || die "в $2 нет записи для $1"
  want=${line%% *}
  if have sha256sum; then got=$(sha256sum "$1" | cut -d' ' -f1); else got=$(shasum -a 256 "$1" | cut -d' ' -f1); fi
  [ "$want" = "$got" ] || die "контрольная сумма $1 не совпала с $2"
}

install_bin() { # install_bin <файл> <куда>
  mkdir -p "$(dirname "$2")"
  install -m 0755 "$1" "$2"
}

if [ -z "$VERSION" ]; then
  VERSION=$(latest_tag)
  [ -n "$VERSION" ] || die "не удалось определить последний релиз $REPO (релизов ещё нет?)"
fi
case "$VERSION" in v*) ver=${VERSION#v} ;; *) ver=$VERSION ;; esac
say "agctl: $os/$arch · $REPO · релиз $VERSION · каталог $INSTALL_DIR"

if [ -n "$profile_id" ]; then
  # ── комплект организации ────────────────────────────────────────────────
  have unzip || die "нужен unzip"
  bundle="agctl-$profile_id-$ver-$os-$arch.zip"
  sums="checksums-$ver.txt"
  fetch "$sums"
  fetch "$bundle"
  verify "$bundle" "$sums"
  unzip -q "$bundle"
  top="${bundle%.zip}"
  [ -d "$top" ] || die "в $bundle нет каталога $top"
  install_bin "$top/agctl" "$INSTALL_DIR/agctl"
  if [ "$os" = darwin ] && [ -z "${AGCTL_NO_LINUX:-}" ]; then
    mkdir -p "$LIB_DIR" && LIB_DIR=$(cd "$LIB_DIR" && pwd)
    install_bin "$top/agctl-linux-$arch" "$LIB_DIR/agctl-linux-$arch"
    say "✓ linux-сборка для самосборки образа: $LIB_DIR/agctl-linux-$arch"
  fi
  [ -f "$top/profile/profile.yaml" ] || die "в комплекте нет profile/profile.yaml"
  mkdir -p "$(dirname "$PROFILE_DIR")"
  rm -rf "$PROFILE_DIR"
  cp -R "$top/profile" "$PROFILE_DIR"
  say "✓ профиль $profile_id: $PROFILE_DIR"
else
  # ── публичный релиз goreleaser ──────────────────────────────────────────
  main_archive="agctl_${os}_${arch}.tar.gz"
  fetch checksums.txt
  fetch "$main_archive"
  verify "$main_archive" checksums.txt
  tar xzf "$main_archive" agctl
  install_bin agctl "$INSTALL_DIR/agctl"
  # На Linux сам бинарь и есть linux-сборка для образа; на macOS она нужна отдельно.
  if [ "$os" = darwin ] && [ -z "${AGCTL_NO_LINUX:-}" ]; then
    linux_archive="agctl_linux_${arch}.tar.gz"
    fetch "$linux_archive"
    verify "$linux_archive" checksums.txt
    rm -f agctl
    tar xzf "$linux_archive" agctl
    mkdir -p "$LIB_DIR" && LIB_DIR=$(cd "$LIB_DIR" && pwd)
    install_bin agctl "$LIB_DIR/agctl-linux-$arch"
    say "✓ linux-сборка для самосборки образа: $LIB_DIR/agctl-linux-$arch"
  fi
fi

if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$INSTALL_DIR/agctl" 2>/dev/null || true
fi
say "✓ бинарь: $INSTALL_DIR/agctl"
say "✓ $("$INSTALL_DIR/agctl" version)"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) say "ℹ $INSTALL_DIR нет в PATH — добавьте в профиль оболочки, например: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
if [ -n "$profile_id" ]; then
  say "Дальше: agctl dev init --profile-dir \"$PROFILE_DIR\" [--flavor …] [--workdir …], затем agctl dev up"
else
  say "Дальше: agctl dev init, затем agctl dev up"
fi
