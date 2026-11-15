# ── map: карта системы ───────────────────────────────────────────────────────
# Карты archify; каждая — вкладка страницы /map работающего сервера (за сессией):
#   agctl-processes — сервер: процессы, порты, протоколы;
#   agctl-local     — локальный режим разработчика (agctl dev);
#   agctl-cluster   — кластер: --runtime k8s | ax.
# Источник — <имя>.architecture.json рядом; HTML собирает archify
# (finalize architecture … --quality showcase), sha256 спецификации, из
# которой собран HTML, лежит в <имя>.delivery.json.
#
# make map        — проверить спецификации (bin/map-check.py), пересобрать
#                   через archify, если он есть и HTML отстал, и открыть
#                   карту MAP_NAME (по умолчанию agctl-processes).
# make map_build  — пересобрать HTML всех карт archify (ARCHIFY=команда, например
#                   ARCHIFY="node ~/src/dot-agents/skills/archify/bin/archify.mjs").
# make map_check  — только проверка: улики против кода, HTML против спецификации.
#
# open вызывается с уникальным #фрагментом: браузер считает URL новым и
# открывает свежую вкладку с диска, а не переиспользует старую вкладку с
# устаревшим рендером.

MAP_DIR     := system/map
MAP_NAMES   := agctl-processes agctl-local agctl-cluster
MAP_NAME    ?= agctl-processes
MAP_FILE    := $(MAP_DIR)/$(MAP_NAME).html
ARCHIFY     ?= archify
# доказательства browser-check archify — вне репозитория
ARCHIFY_OUT ?= /tmp/agctl-map-review

.PHONY: map map_build map_check

## map: открыть карту (MAP_NAME=…) в браузере (проверка + пересборка archify, если есть)
map:
	@rc=0; $(MAKE) --no-print-directory map_check || rc=$$?; \
	if [ $$rc -eq 1 ]; then exit 1; fi; \
	if [ $$rc -eq 2 ] && command -v $(firstword $(ARCHIFY)) >/dev/null 2>&1; then $(MAKE) --no-print-directory map_build; fi; \
	if [ ! -f "$(MAP_FILE)" ]; then echo "$(MAP_FILE) не найден — make map_build (нужен archify)"; exit 1; fi; \
	stamp=$$(date -r "$(MAP_FILE)" '+%Y-%m-%d %H:%M' 2>/dev/null || stat -c '%y' "$(MAP_FILE)" 2>/dev/null | cut -d. -f1 || echo '?'); \
	esc=$$(printf '%s' "$(CURDIR)/$(MAP_FILE)" | sed 's/ /%20/g'); \
	if command -v open >/dev/null 2>&1; then \
	  open "file://$$esc#$$(date +%s)" 2>/dev/null || open "$(MAP_FILE)"; \
	elif command -v xdg-open >/dev/null 2>&1; then xdg-open "file://$$esc#$$(date +%s)"; \
	else echo "$(CURDIR)/$(MAP_FILE)"; fi; \
	echo "карта: сборка HTML от $$stamp (если вкладка выглядит старой — Cmd+Shift+R)"

## map_build: пересобрать HTML всех карт archify из спецификаций (ARCHIFY=…)
map_build:
	@command -v $(firstword $(ARCHIFY)) >/dev/null 2>&1 || { \
	  echo "archify не найден (ARCHIFY=$(ARCHIFY)): HTML карты собирается только им — см. system/embed.go"; exit 1; }
	@cd $(MAP_DIR) && for n in $(MAP_NAMES); do \
	  echo "archify: $$n"; \
	  $(ARCHIFY) finalize architecture $$n.architecture.json $$n.html --quality showcase \
	    --repo-root "$(CURDIR)" --out-dir "$(ARCHIFY_OUT)/$$n-$$(date +%s)" || exit 1; \
	done

## map_check: проверить спецификации карт (улики против кода, HTML против спецификации)
map_check:
	@rc=0; for n in $(MAP_NAMES); do \
	  r=0; python3 -I bin/map-check.py "$(MAP_DIR)/$$n.architecture.json" "$(MAP_DIR)/$$n.delivery.json" || r=$$?; \
	  if [ $$r -gt $$rc ]; then rc=$$r; fi; \
	done; exit $$rc
