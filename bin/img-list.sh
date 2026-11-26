#!/bin/sh
# img-list.sh — образы рабочего места на этой машине и что в них внутри:
# версия dsh, сборка guest agctl, уровень API guest, вид образа (full | min |
# web), дата сборки и размер. Данные — из меток io.agctl.* (Dockerfile,
# последний слой); у образов, собранных до меток, они пустые — «без меток».
# Вызывается make img_list и в конце make img_build / img_dsh.
#
#   sh bin/img-list.sh [префикс]   # по умолчанию agctl-dsh
set -eu

prefix="${1:-agctl-dsh}"
tab="$(printf '\t')"
# column выравнивает кириллицу только в UTF-8 локали.
loc="$(locale -a 2>/dev/null | grep -i -m1 -E '^(c|en_us)\.utf-?8$' || echo C)"

{
	printf 'ОБРАЗ\tDSH\tGUEST\tAPI\tВИД\tСОБРАН\tРАЗМЕР\n'
	docker images --format '{{.Repository}}:{{.Tag}}' | grep -E "^${prefix}" | sort | while read -r ref; do
		docker image inspect "$ref" --format \
			'{{index .Config.Labels "io.agctl.dsh.version"}}|{{index .Config.Labels "io.agctl.guest.revision"}}|{{index .Config.Labels "io.agctl.guest.api"}}|{{index .Config.Labels "io.agctl.image.variant"}}|{{index .Config.Labels "org.opencontainers.image.created"}}|{{.Size}}' |
			awk -F'|' -v ref="$ref" '{
				printf "%s\t%s\t%s\t%s\t%s\t%s\t%.2f ГБ\n", ref,
					($1 == "" ? "?" : $1), ($2 == "" ? "без меток" : $2), ($3 == "" ? "?" : $3),
					($4 == "" ? "?" : $4), ($5 == "" ? "?" : $5), $6 / 1e9
			}'
	done
} | LC_ALL="$loc" column -t -s "$tab"
