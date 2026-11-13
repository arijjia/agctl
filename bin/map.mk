# ── map: карта системы ───────────────────────────────────────────────────────
# Физическая карта (процессы, порты, протоколы): agctl-processes.html, она же
# раздаётся работающим сервером как страница /map за сессией. Источник —
# agctl-processes.architecture.json рядом; HTML собирает archify
# (finalize architecture … --quality showcase), sha256 спецификации, из
# которой собран HTML, лежит в agctl-processes.delivery.json.
#
# make map        — проверить спецификацию (bin/map-check.py), пересобрать
#                   через archify, если он есть и HTML отстал, и открыть.
# make map_build  — пересобрать HTML archify (ARCHIFY=путь/к/archify).
# make map_check  — только проверка: улики против кода, HTML против спецификации.
#
# open вызывается с уникальным #фрагментом: браузер считает URL новым и
# открывает свежую вкладку с диска, а не переиспользует старую вкладку с
# устаревшим рендером.

MAP_DIR     := system/map
MAP_NAME    := agctl-processes
MAP_SPEC    := $(MAP_DIR)/$(MAP_NAME).architecture.json
MAP_FILE    := $(MAP_DIR)/$(MAP_NAME).html
MAP_RECEIPT := $(MAP_DIR)/$(MAP_NAME).delivery.json
ARCHIFY     ?= archify

.PHONY: map map_build map_check

## map: открыть карту системы в браузере (проверка + пересборка archify, если есть)
map:
	@rc=0; python3 -I bin/map-check.py "$(MAP_SPEC)" "$(MAP_RECEIPT)" || rc=$$?; \
	if [ $$rc -eq 1 ]; then exit 1; fi; \
	if [ $$rc -eq 2 ] && command -v "$(ARCHIFY)" >/dev/null 2>&1; then $(MAKE) --no-print-directory map_build; fi; \
	if [ ! -f "$(MAP_FILE)" ]; then echo "$(MAP_FILE) не найден — make map_build (нужен archify)"; exit 1; fi; \
	stamp=$$(date -r "$(MAP_FILE)" '+%Y-%m-%d %H:%M' 2>/dev/null || stat -c '%y' "$(MAP_FILE)" 2>/dev/null | cut -d. -f1 || echo '?'); \
	esc=$$(printf '%s' "$(CURDIR)/$(MAP_FILE)" | sed 's/ /%20/g'); \
	if command -v open >/dev/null 2>&1; then \
	  open "file://$$esc#$$(date +%s)" 2>/dev/null || open "$(MAP_FILE)"; \
	elif command -v xdg-open >/dev/null 2>&1; then xdg-open "file://$$esc#$$(date +%s)"; \
	else echo "$(CURDIR)/$(MAP_FILE)"; fi; \
	echo "карта: сборка HTML от $$stamp (если вкладка выглядит старой — Cmd+Shift+R)"

## map_build: пересобрать HTML карты archify из спецификации (ARCHIFY=…)
map_build:
	@command -v "$(ARCHIFY)" >/dev/null 2>&1 || { \
	  echo "archify не найден (ARCHIFY=$(ARCHIFY)): HTML карты собирается только им — см. system/embed.go"; exit 1; }
	cd $(MAP_DIR) && "$(ARCHIFY)" finalize architecture $(MAP_NAME).architecture.json $(MAP_NAME).html --quality showcase

## map_check: проверить спецификацию карты (улики против кода, HTML против спецификации)
map_check:
	@python3 -I bin/map-check.py "$(MAP_SPEC)" "$(MAP_RECEIPT)"
