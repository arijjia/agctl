# ── map: карта системы ───────────────────────────────────────────────────────
# Собранная archify-диаграмма (system/map/overview.html), она же раздаётся
# работающим сервером как страница /map за сессией.

MAP_FILE := system/map/overview.html

.PHONY: map

## map: открыть карту системы в браузере
map:
	@if [ -f "$(MAP_FILE)" ]; then \
	  if command -v open >/dev/null 2>&1; then open "$(MAP_FILE)"; \
	  elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$(MAP_FILE)"; \
	  else echo "$(CURDIR)/$(MAP_FILE)"; fi; \
	else echo "$(MAP_FILE) не найден — карта собирается archify (см. system/embed.go)"; exit 1; fi
