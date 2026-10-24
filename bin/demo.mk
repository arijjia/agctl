# ── demo: изолированный демо-стенд «с ноута за 10 минут» ─────────────────────
# Поднимает платформу на временном доме (/tmp/agctl-demo) и отдельном порту —
# не трогает ни ~/.agctl, ни инстанс на :8929. Данные переживают перезапуск,
# пока /tmp/agctl-demo не удалён (demo_fresh — с чистого листа).

DEMO_HOME ?= /tmp/agctl-demo
DEMO_ADDR ?= 127.0.0.1:8930
DEMO_LOG := $(RUNTIME_DIR)/demo.log
DEMO_PID := $(RUNTIME_DIR)/demo.pid

.PHONY: demo demo_fresh demo_clean

## demo: собрать всё и поднять изолированный демо-стенд (дом /tmp, порт 8930)
demo: web_build go_build img_build | $(RUNTIME_DIR)
	@if [ -f "$(DEMO_PID)" ] && kill -0 "$$(cat $(DEMO_PID))" 2>/dev/null; then \
	  echo "демо-стенд уже работает: http://$(DEMO_ADDR) (pid $$(cat $(DEMO_PID)))"; exit 1; fi
	@AGCTL_HOME="$(DEMO_HOME)" nohup ./dist/agctl serve --addr "$(DEMO_ADDR)" >>"$(DEMO_LOG)" 2>&1 & echo $$! > "$(DEMO_PID)"
	@sleep 1
	@if kill -0 "$$(cat $(DEMO_PID))" 2>/dev/null; then \
	  echo "✓ демо-стенд: http://$(DEMO_ADDR)"; \
	  echo "  дом: $(DEMO_HOME) (изолирован от ~/.agctl) · лог: $(DEMO_LOG) · остановка: make stop"; \
	else \
	  echo "✗ демо-стенд упал при старте — хвост $(DEMO_LOG):"; tail -5 "$(DEMO_LOG)"; exit 1; fi

## demo_fresh: пересоздать демо-стенд с чистого листа
demo_fresh: demo_clean demo

## demo_clean: удалить дом демо-стенда (включая учётки и сессии демо)
demo_clean:
	rm -rf $(DEMO_HOME)
	@echo "демо-дом $(DEMO_HOME) удалён"
