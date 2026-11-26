#!/bin/sh
# agctl — установка бинаря одной командой (macOS, Linux):
#
#   curl -fsSL https://raw.githubusercontent.com/arijjia/agctl/main/bin/install.sh | sh
#
# Два источника:
#   • публичный релиз agctl (goreleaser): архивы agctl_<os>_<arch>.tar.gz,
#     agctl-guest_linux_<arch>.tar.gz (локальный guest: bridge) +
#     agctl-guest-server_linux_<arch>.tar.gz (серверный guest: ax-runner,
#     init-chat-home) + checksums.txt;
#   • комплект организации — релиз приватного репозитория профиля
#     AGCTL_REPO=<org>/agctl-profile-<id>: zip agctl-<id>-<версия>-<os>-<arch>.zip
#     (agctl + agctl-guest-linux-<arch> + agctl-guest-server-linux-<arch> +
#     profile/) + checksums-<версия>.txt; нужен авторизованный gh и unzip.
#
# Всё ставится под один корень — ~/.agctl (переменная AGCTL_ROOT); раскладка
# та же, что в internal/localmode/layout.go:
#
#   <корень>/bin/agctl                              бинарь (этот каталог — в PATH)
#   <корень>/bin/guest/agctl-guest-linux-<arch>     guest локального образа agctl-dsh*
#   <корень>/bin/guest/agctl-guest-server-linux-<arch>  guest серверного образа
#   <корень>/profiles/<id>/                         профиль комплекта организации
#   <корень>/homes/<id>/                            дом (создаёт agctl dev init)
#
# Что делает: определяет ОС и архитектуру, скачивает архив и контрольные
# суммы (через gh, если он есть и авторизован, иначе curl), сверяет SHA-256,
# кладёт бинарь и оба agctl-guest (нужны только для самосборки образа без
# реестра), снимает карантин macOS, печатает agctl version. Комплект
# организации: профиль кладётся в <корень>/profiles/<id>, и скрипт сам
# создаёт для него СВОЙ дом homes/<id> (agctl dev --home <id> init --profile
# <id> --yes) — дом по умолчанию и другие дома не трогаются; AGCTL_NO_INIT=1
# — не создавать, напечатать команду. Сервер организации (agctl-server) этот
# скрипт не ставит: он в архивах agctl-server_<os>_<arch>.
#
# Переменные окружения:
#   AGCTL_REPO         репозиторий GitHub (по умолчанию arijjia/agctl;
#                      <org>/agctl-profile-<id> — комплект организации)
#   AGCTL_VERSION      тег релиза (по умолчанию последний)
#   AGCTL_ROOT         корень установки (по умолчанию ~/.agctl)
#   AGCTL_WORKDIR      папка проектов для авто-init комплекта (по умолчанию ~/src)
#   AGCTL_RELEASE_DIR  локальная папка с архивами релиза — ничего не скачивать
#   AGCTL_NO_LINUX=1   не ставить agctl-guest (образы берутся из реестра)
#   AGCTL_NO_INIT=1    не создавать дом комплекта самим — напечатать команду
#
# Windows — bin/install.ps1.
set -eu

REPO="${AGCTL_REPO:-arijjia/agctl}"
VERSION="${AGCTL_VERSION:-}"
ROOT="${AGCTL_ROOT:-$HOME/.agctl}"
mkdir -p "$ROOT" && ROOT=$(cd "$ROOT" && pwd)
export AGCTL_ROOT="$ROOT" # agctl ниже работает с этим же корнем
BIN_DIR="$ROOT/bin"
GUEST_DIR="$BIN_DIR/guest"

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
PROFILE_DIR="$ROOT/profiles/$profile_id"

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
say "agctl: $os/$arch · $REPO · релиз $VERSION · корень $ROOT"

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
  install_bin "$top/agctl" "$BIN_DIR/agctl"
  if [ -z "${AGCTL_NO_LINUX:-}" ]; then
    guest="agctl-guest-linux-$arch"
    if [ -f "$top/$guest" ]; then
      install_bin "$top/$guest" "$GUEST_DIR/$guest"
      say "✓ бинарь образа локального режима: $GUEST_DIR/$guest"
    else
      say "ℹ в комплекте нет $guest — образ рабочего места только из реестра"
    fi
    # Серверный guest (agctl-dsh-server*); его отсутствие не фатально.
    if [ -f "$top/agctl-guest-server-linux-$arch" ]; then
      install_bin "$top/agctl-guest-server-linux-$arch" "$GUEST_DIR/agctl-guest-server-linux-$arch"
      say "✓ бинарь образа серверного режима: $GUEST_DIR/agctl-guest-server-linux-$arch"
    fi
  fi
  [ -f "$top/profile/profile.yaml" ] || die "в комплекте нет profile/profile.yaml"
  mkdir -p "$ROOT/profiles"
  if [ -L "$PROFILE_DIR" ]; then
    # Работа из исходников: profiles/<id> — ссылка на репозиторий профиля.
    say "ℹ $PROFILE_DIR — ссылка на $(cd "$PROFILE_DIR" 2>/dev/null && pwd -P || echo '?'); профиль комплекта не распакован, ссылка оставлена"
  else
    rm -rf "$PROFILE_DIR"
    cp -R "$top/profile" "$PROFILE_DIR"
    say "✓ профиль $profile_id: $PROFILE_DIR"
  fi
else
  # ── публичный релиз goreleaser ──────────────────────────────────────────
  main_archive="agctl_${os}_${arch}.tar.gz"
  fetch checksums.txt
  fetch "$main_archive"
  verify "$main_archive" checksums.txt
  tar xzf "$main_archive" agctl
  install_bin agctl "$BIN_DIR/agctl"
  # Бинари образа рабочего места — agctl-guest (linux), по одному на режим,
  # на любой ОС хоста.
  if [ -z "${AGCTL_NO_LINUX:-}" ]; then
    guest_archive="agctl-guest_linux_${arch}.tar.gz"
    fetch "$guest_archive"
    verify "$guest_archive" checksums.txt
    tar xzf "$guest_archive" agctl-guest
    install_bin agctl-guest "$GUEST_DIR/agctl-guest-linux-$arch"
    say "✓ бинарь образа локального режима: $GUEST_DIR/agctl-guest-linux-$arch"
    server_archive="agctl-guest-server_linux_${arch}.tar.gz"
    fetch "$server_archive"
    verify "$server_archive" checksums.txt
    tar xzf "$server_archive" agctl-guest
    install_bin agctl-guest "$GUEST_DIR/agctl-guest-server-linux-$arch"
    say "✓ бинарь образа серверного режима: $GUEST_DIR/agctl-guest-server-linux-$arch"
  fi
fi

if [ "$os" = darwin ]; then
  xattr -d com.apple.quarantine "$BIN_DIR/agctl" 2>/dev/null || true
fi
say "✓ бинарь: $BIN_DIR/agctl"
say "✓ $("$BIN_DIR/agctl" version)"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) say "ℹ $BIN_DIR нет в PATH — добавьте в профиль оболочки: export PATH=\"$BIN_DIR:\$PATH\"  (fish: fish_add_path $BIN_DIR)" ;;
esac
if [ -n "$profile_id" ] && [ -z "${AGCTL_NO_INIT:-}" ]; then
  # Свой дом комплекта — homes/<id>; дом по умолчанию и чужие дома не
  # трогаются (--yes — не спрашивать; доступы спросит лаунчер).
  set -- dev --home "$profile_id" init --profile "$profile_id" --yes
  if [ -n "${AGCTL_WORKDIR:-}" ]; then set -- "$@" --workdir "$AGCTL_WORKDIR"; fi
  "$BIN_DIR/agctl" "$@" || say "⚠ agctl dev init не удался — создайте дом сами: agctl dev --home $profile_id init --profile $profile_id"
  say "Дальше: agctl dev --home $profile_id up   (где что лежит — agctl dev --home $profile_id paths)"
elif [ -n "$profile_id" ]; then
  say "Дальше: agctl dev --home $profile_id init --profile $profile_id [--flavor …] [--workdir …], затем agctl dev --home $profile_id up"
else
  say "Дальше: agctl dev init, затем agctl dev up   (где что лежит — agctl dev paths)"
fi
case "$(ls -d "$HOME"/.agctl-dev "$HOME"/.agctl-dev-* 2>/dev/null | head -1)" in
  "") ;;
  *) say "ℹ найдены прежние дома ~/.agctl-dev* — перенос в $ROOT/homes: agctl dev migrate (план), agctl dev migrate --apply" ;;
esac
