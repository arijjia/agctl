#!/bin/sh
# agctl — установка бинаря одной командой (macOS, Linux):
#
#   curl -fsSL https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.sh | sh
#
# Два источника:
#   • публичный релиз agctl (goreleaser): архивы agctl_<os>_<arch>.tar.gz,
#     agctl-guest_linux_<arch>.tar.gz + checksums.txt;
#   • комплект организации — релиз приватного репозитория профиля
#     AGCTL_REPO=<org>/agctl-profile-<id>: zip agctl-<id>-<версия>-<os>-<arch>.zip
#     (agctl + agctl-guest-linux-<arch> + profile/) + checksums-<версия>.txt; нужен
#     авторизованный gh и unzip. Профиль кладётся в
#     ~/.local/share/agctl/profiles/<id>; в конце скрипт сам подключает
#     профиль (agctl dev init --profile-dir … --yes; AGCTL_NO_INIT=1 —
#     не подключать, напечатать команду).
#
# Что делает: определяет ОС и архитектуру, скачивает архив и контрольные
# суммы (через gh, если он есть и авторизован, иначе curl), сверяет SHA-256,
# кладёт бинарь в ~/.local/bin, снимает карантин macOS, кладёт agctl-guest —
# linux-бинарь образа рабочего места — в ~/.local/lib/agctl (нужен только для
# самосборки образа без реестра), печатает agctl version. Сервер организации
# (agctl-server) этот скрипт не ставит: он в архивах agctl-server_<os>_<arch>.
#
# Переменные окружения:
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl;
#                      <org>/agctl-profile-<id> — комплект организации)
#   AGCTL_VERSION      тег релиза (по умолчанию последний)
#   AGCTL_INSTALL_DIR  куда ставить бинарь (по умолчанию ~/.local/bin)
#   AGCTL_LIB_DIR      куда класть agctl-guest (по умолчанию <INSTALL_DIR>/../lib/agctl)
#   AGCTL_PROFILE_DIR  куда класть профиль комплекта (по умолчанию
#                      ${XDG_DATA_HOME:-~/.local/share}/agctl/profiles/<id>)
#   AGCTL_WORKDIR      папка проектов для авто-init комплекта (по умолчанию ~/src)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить agctl-guest (образы берутся из реестра)
#   AGCTL_NO_INIT=1    не подключать профиль комплекта самим — напечатать команду
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
  if [ -z "${AGCTL_NO_LINUX:-}" ]; then
    mkdir -p "$LIB_DIR" && LIB_DIR=$(cd "$LIB_DIR" && pwd)
    # Комплекты до разделения бинарей несут agctl-linux-<arch> (единый бинарь,
    # те же команды образа); findLinuxBinary понимает оба имени.
    guest="agctl-guest-linux-$arch"
    [ -f "$top/$guest" ] || guest="agctl-linux-$arch"
    install_bin "$top/$guest" "$LIB_DIR/$guest"
    say "✓ бинарь образа для самосборки: $LIB_DIR/$guest"
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
  # Бинарь образа рабочего места — отдельный agctl-guest (linux), на любой ОС хоста.
  if [ -z "${AGCTL_NO_LINUX:-}" ]; then
    guest_archive="agctl-guest_linux_${arch}.tar.gz"
    fetch "$guest_archive"
    verify "$guest_archive" checksums.txt
    tar xzf "$guest_archive" agctl-guest
    mkdir -p "$LIB_DIR" && LIB_DIR=$(cd "$LIB_DIR" && pwd)
    install_bin agctl-guest "$LIB_DIR/agctl-guest-linux-$arch"
    rm -f "$LIB_DIR/agctl-linux-$arch"
    say "✓ бинарь образа для самосборки: $LIB_DIR/agctl-guest-linux-$arch"
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
if [ -n "$profile_id" ] && [ -z "${AGCTL_NO_INIT:-}" ]; then
  # Подключить профиль самим (--yes — не спрашивать; доступы спросит лаунчер).
  set -- dev init --profile-dir "$PROFILE_DIR" --yes
  if [ -n "${AGCTL_WORKDIR:-}" ]; then set -- "$@" --workdir "$AGCTL_WORKDIR"; fi
  "$INSTALL_DIR/agctl" "$@" || say "⚠ agctl dev init не удался — подключите профиль сами: agctl dev init --profile-dir \"$PROFILE_DIR\""
elif [ -n "$profile_id" ]; then
  say "Дальше: agctl dev init --profile-dir \"$PROFILE_DIR\" [--flavor …] [--workdir …], затем agctl dev up"
else
  say "Дальше: agctl dev init, затем agctl dev up"
fi
