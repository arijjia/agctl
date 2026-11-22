#!/usr/bin/env bash
# local.sh — локальный режим (agctl dev) на машине разработчика agctl:
# несколько независимых установок рядом — по дому на профиль.
#
#   PROFILE=agctl (по умолчанию) — встроенный нейтральный профиль:
#       дом ~/.agctl-dev, порт 8930.
#   PROFILE=<id> — профиль организации из сиблинга ../agctl-profile-<id>
#       (или LOCAL_PROFILE_DIR=…): дом ~/.agctl-dev-<id>, порт — первый
#       свободный с 8931, дальше живёт в dev.yaml этого дома. Ссылка
#       profiles/<id> → сиблинг создаётся сама (её читают make img_*).
#
# Команды: up | init | down | status | logs | open | doctor | update | list
# Бинарь — тот, что в PATH (make install ставит его в ~/.local/bin).
#
# Переменные: LOCAL_WORKDIR — папка проектов при первом init (по умолчанию
# ~/src), LOCAL_PROFILE_DIR — каталог профиля вместо сиблинга,
# LOCAL_SECRETS=file — секреты дома в файле вместо общего хранилища ОС.
set -euo pipefail

cmd=${1:-}
profile=${2:-agctl}
root=$(cd "$(dirname "$0")/.." && pwd)
agctl_bin=$(command -v agctl || true)
[ -n "$agctl_bin" ] || { echo "✗ agctl не найден в PATH — make install"; exit 1; }

home_of() { if [ "$1" = agctl ]; then echo "$HOME/.agctl-dev"; else echo "$HOME/.agctl-dev-$1"; fi; }
home=$(home_of "$profile")
export AGCTL_DEV_HOME="$home"
[ "${LOCAL_SECRETS:-}" = file ] && export AGCTL_DEV_SECRETS=file

# addr из dev.yaml дома (пусто — не инициализирован).
addr_of() { sed -n 's/^addr: *//p' "$1/dev.yaml" 2>/dev/null | head -1; }

profile_dir() {
	local d=${LOCAL_PROFILE_DIR:-$root/../agctl-profile-$profile}
	[ -f "$d/profile.yaml" ] || d=$root/profiles/$profile
	[ -f "$d/profile.yaml" ] || { echo "✗ профиль $profile не найден: ../agctl-profile-$profile или profiles/$profile (LOCAL_PROFILE_DIR=…)" >&2; return 1; }
	(cd "$d" && pwd -P)
}

link_profile() {
	local d=$1
	mkdir -p "$root/profiles"
	if [ ! -e "$root/profiles/$profile" ]; then
		ln -s "$d" "$root/profiles/$profile" && echo "  profiles/$profile → $d"
	fi
}

# Порты других домов (свой дом не считается).
ports_of_others() {
	local f
	for f in "$HOME"/.agctl-dev/dev.yaml "$HOME"/.agctl-dev-*/dev.yaml; do
		[ -f "$f" ] && [ "$(dirname "$f")" != "$home" ] && addr_of "$(dirname "$f")"
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
	local args=(--yes)
	if [ "$profile" = agctl ]; then
		args+=(--profile agctl)
	else
		local d; d=$(profile_dir)
		link_profile "$d"
		args+=(--profile-dir "$d")
	fi
	local cur; cur=$(addr_of "$home")
	if [ "$profile" = agctl ]; then
		[ -n "$cur" ] || args+=(--addr 127.0.0.1:8930)
	elif [ -z "$cur" ] || grep -qx "${cur##*:}" <<<"$(ports_of_others)"; then
		# новый дом или порт занят другим домом (например, дом перенесён из ~/.agctl-dev)
		[ -n "$cur" ] && echo "  порт ${cur##*:} занят другим домом — выбираю свободный"
		args+=(--addr "127.0.0.1:$(free_port)")
	fi
	[ -n "${LOCAL_WORKDIR:-}" ] && args+=(--workdir "$LOCAL_WORKDIR")
	echo "agctl dev init (дом $home): ${args[*]}"
	agctl dev init "${args[@]}"
}

# Чужая незакоммиченная работа в чекауте попадёт в бинарь — предупредить.
warn_dirty() {
	if ! git -C "$root" diff --quiet HEAD 2>/dev/null; then
		echo "⚠ в $root есть незакоммиченные правки — make install собрал их в бинарь"
	fi
}

case "$cmd" in
up)
	warn_dirty
	do_init
	agctl dev up
	;;
init) do_init ;;
down | status | open | doctor) agctl dev "$cmd" ;;
logs) agctl dev logs -f ;;
update) agctl dev update ;;
list)
	printf '%-10s %-22s %-16s %s\n' ПРОФИЛЬ ДОМ АДРЕС СОСТОЯНИЕ
	for f in "$HOME"/.agctl-dev/dev.yaml "$HOME"/.agctl-dev-*/dev.yaml; do
		[ -f "$f" ] || continue
		h=$(dirname "$f")
		id=${h##*/.agctl-dev}; id=${id#-}; id=${id:-agctl}
		st=$(AGCTL_DEV_HOME=$h agctl dev status 2>/dev/null | sed -n 's/^статус: *//p' | head -1)
		printf '%-10s %-22s %-16s %s\n' "$id" "${h/#$HOME/~}" "$(addr_of "$h")" "${st:-?}"
	done
	;;
*)
	echo "использование: bin/local.sh up|init|down|status|logs|open|doctor|update|list [профиль]"
	exit 2
	;;
esac
