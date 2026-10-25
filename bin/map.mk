# ── map: карта системы ───────────────────────────────────────────────────────
# Собранная archify-диаграмма (system/map/overview.html), она же раздаётся
# работающим сервером как страница /map за сессией.
#
# open вызывается с уникальным #фрагментом: браузер считает URL новым и
# открывает свежую вкладку с диска, а не переиспользует старую вкладку с
# устаревшим рендером.

MAP_FILE := system/map/overview.html

.PHONY: map

## map: открыть карту системы в браузере (всегда свежую вкладку)
map:
	@if [ -f "$(MAP_FILE)" ]; then \
	  stamp=$$(date -r "$(MAP_FILE)" '+%Y-%m-%d %H:%M' 2>/dev/null || stat -c '%y' "$(MAP_FILE)" 2>/dev/null | cut -d. -f1 || echo '?'); \
	  if command -v open >/dev/null 2>&1; then open "$(MAP_FILE)#$$(date +%s)"; \
	  elif command -v xdg-open >/dev/null 2>&1; then xdg-open "$(MAP_FILE)#$$(date +%s)"; \
	  else echo "$(CURDIR)/$(MAP_FILE)"; fi; \
	  echo "карта: сборка от $$stamp (если вкладка выглядит старой — Cmd+Shift+R)"; \
	else echo "$(MAP_FILE) не найден — карта собирается archify (см. system/embed.go)"; exit 1; fi
