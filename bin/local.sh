#!/usr/bin/env bash
# local.sh — локальный режим (agctl dev) на машине разработчика agctl:
# несколько независимых установок рядом — по дому на профиль. Всё лежит под
# одним корнем (~/.agctl или AGCTL_ROOT; раскладка — internal/localmode/layout.go):
#
#   <корень>/bin/agctl            бинарь (ставит make install)
#   <корень>/profiles/<id>        ссылка на сиблинг-репо ../agctl-profile-<id>
#   <корень>/homes/<id>/          дом профиля: config.yaml, data/, logs/, run/
#
#   PROFILE=agctl (по умолчанию) — встроенный нейтральный профиль:
#       дом homes/agctl, порт 8930.
#   PROFILE=<id> — профиль организации из сиблинга ../agctl-profile-<id>
#       (или LOCAL_PROFILE_DIR=…): дом homes/<id>, порт — первый свободный
#       с 8931, дальше живёт в config.yaml этого дома. Ссылка
#       <корень>/profiles/<id> → сиблинг создаётся сама: профиль один, его
#       читают и agctl dev, и make img_*; внутри репозитория ссылок нет.
#
# Команды: up | init | down | status | paths | logs | open | doctor | update | list
#
# Переменные: LOCAL_WORKDIR — папка проектов при первом init (по умолчанию
# ~/src), LOCAL_PROFILE_DIR — каталог профиля вместо сиблинга,
# LOCAL_SECRETS=file — секреты дома в файле вместо хранилища ОС.
set -euo pipefail

cmd=${1:-}
profile=${2:-agctl}
root=$(cd "$(dirname "$0")/.." && pwd)
agctl_root=${AGCTL_ROOT:-$HOME/.agctl}
export AGCTL_ROOT="$agctl_root"
# Дом задаётся явно (--home): переменные выбора дома из окружения не мешают.
unset AGCTL_DEV_HOME AGCTL_HOME_ID
agctl_bin="$agctl_root/bin/agctl"
[ -x "$agctl_bin" ] || { echo "✗ нет $agctl_bin — make install"; exit 1; }
agctl() { "$agctl_bin" "$@"; }

homes="$agctl_root/homes"
home="$homes/$profile"
[ "${LOCAL_SECRETS:-}" = file ] && export AGCTL_DEV_SECRETS=file

# Конфиг дома: config.yaml (или прежний dev.yaml); пусто — дом не настроен.
config_of() {
	if [ -f "$1/config.yaml" ]; then echo "$1/config.yaml"; elif [ -f "$1/dev.yaml" ]; then echo "$1/dev.yaml"; fi
}
# addr из конфига дома (пусто — не инициализирован).
addr_of() {
	local c; c=$(config_of "$1")
	[ -n "$c" ] || return 0
	sed -n 's/^addr: *//p' "$c" | head -1
}

profile_dir() {
	local d=${LOCAL_PROFILE_DIR:-$root/../agctl-profile-$profile}
	[ -f "$d/profile.yaml" ] || { echo "✗ профиль $profile не найден: $d (сиблинг ../agctl-profile-$profile или LOCAL_PROFILE_DIR=…)" >&2; return 1; }
	(cd "$d" && pwd -P)
}

# <корень>/profiles/<id> → репозиторий профиля. Чужое (распакованный комплект
# или ссылка в другое место) не перезаписываем — говорим, что там.
link_profile() {
	local d=$1 link="$agctl_root/profiles/$profile"
	mkdir -p "$agctl_root/profiles"
	if [ -L "$link" ]; then
		local cur; cur=$(cd "$link" 2>/dev/null && pwd -P || true)
		[ "$cur" = "$d" ] && return 0
		echo "✗ $link указывает на ${cur:-несуществующий каталог}, а не на $d — уберите ссылку и повторите" >&2
		return 1
	fi
	if [ -e "$link" ]; then
		echo "✗ $link — каталог (распакованный комплект?), а профиль берётся из $d. Уберите каталог и повторите" >&2
		return 1
	fi
	ln -s "$d" "$link" && echo "  $link → $d"
}

# Порты других домов (свой дом не считается).
ports_of_others() {
	local h
	for h in "$homes"/*/; do
		h=${h%/}
		[ -d "$h" ] && [ "$h" != "$home" ] && addr_of "$h"
	done | sed 's/.*://'
	echo 8930 # закреплён за нейтральным agctl
}

free_port() {
	local p=8931 used
	used=$(ports_of_others)
	while grep -qx "$p" <<<"$used" || lsof -nP -iTCP:"$p" -sTCP:LISTEN >/dev/null 2>&1; do p=$((p + 1)); done
	echo "$p"
}

do_init() {
	local args=(--yes --profile "$profile")
	if [ "$profile" != agctl ]; then
		local d; d=$(profile_dir)
		link_profile "$d"
	fi
	local cur; cur=$(addr_of "$home")
	if [ "$profile" = agctl ]; then
		[ -n "$cur" ] || args+=(--addr 127.0.0.1:8930)
	elif [ -z "$cur" ] || grep -qx "${cur##*:}" <<<"$(ports_of_others)"; then
		# новый дом или порт занят другим домом
		[ -n "$cur" ] && echo "  порт ${cur##*:} занят другим домом — выбираю свободный"
		args+=(--addr "127.0.0.1:$(free_port)")
	fi
	[ -n "${LOCAL_WORKDIR:-}" ] && args+=(--workdir "$LOCAL_WORKDIR")
	echo "agctl dev --home $profile init ${args[*]}"
	agctl dev --home "$profile" init "${args[@]}"
}

# Чужая незакоммиченная работа в чекауте попадёт в бинарь — предупредить.
warn_dirty() {
	if ! git -C "$root" diff --quiet HEAD 2>/dev/null; then
		echo "⚠ в $root есть незакоммиченные правки — make install собрал их в бинарь"
	fi
}

warn_legacy() {
	if ls -d "$HOME"/.agctl-dev "$HOME"/.agctl-dev-* >/dev/null 2>&1; then
		echo "ℹ найдены прежние дома ~/.agctl-dev* — перенос в $homes: agctl dev migrate (план), затем --apply"
	fi
}

case "$cmd" in
up)
	warn_dirty
	warn_legacy
	do_init
	agctl dev --home "$profile" up
	;;
init) do_init ;;
down | status | open | doctor | paths) agctl dev --home "$profile" "$cmd" ;;
logs) agctl dev --home "$profile" logs -f ;;
update) agctl dev --home "$profile" update ;;
list)
	printf '%-10s %-34s %-16s %s\n' ДОМ ПУТЬ АДРЕС СОСТОЯНИЕ
	for h in "$homes"/*/; do
		h=${h%/}
		[ -n "$(config_of "$h")" ] || continue
		id=${h##*/}
		st=$(agctl dev --home "$id" status 2>/dev/null | sed -n 's/^статус: *//p' | head -1)
		printf '%-10s %-34s %-16s %s\n' "$id" "${h/#$HOME/~}" "$(addr_of "$h")" "${st:-?}"
	done
	;;
*)
	echo "использование: bin/local.sh up|init|down|status|paths|logs|open|doctor|update|list [профиль]"
	exit 2
	;;
esac
