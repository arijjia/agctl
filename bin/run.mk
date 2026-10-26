# ── run: жизненный цикл платформы (up / corp / stop / down / logs / status) ──
# Фоновый запуск с pidfile и логом в .runtime/. `make stop` — аккуратный
# SIGTERM инстансам, поднятым make (сервер сам закрывает чат-контейнеры и
# ждёт http-shutdown); `make down` — то же плюс docker stop контейнеров мест
# (тома на диске остаются, данные не трогаются).

RUN_ADDR ?= 127.0.0.1:8929
RUN_HOME ?= $(HOME)/.agctl
RUN_UP_LOG := $(RUNTIME_DIR)/up.log
RUN_UP_PID := $(RUNTIME_DIR)/up.pid
RUN_CORP_LOG := $(RUNTIME_DIR)/corp.log
RUN_CORP_PID := $(RUNTIME_DIR)/corp.pid

# Дефолты корпоративного провайдера чата; ключ берётся из .env
# (AGCTL_LLM_API_KEY) — он экспортируется корневым Makefile.
CORP_LLM_BASE_URL ?= https://litellm-dev.utmn.ru/v1
CORP_LLM_MODELS ?= qwen-3.8-dee,qwen-3.8-27b,qwen-3.8-fast,qwen3guard,qwen3.5-9b-q8_0
CORP_LLM_MODEL ?= qwen-3.8-fast

.PHONY: up corp stop down restart logs status

$(RUNTIME_DIR):
	@mkdir -p $(RUNTIME_DIR)

## up: собрать и поднять платформу в фоне (RUN_ADDR/RUN_HOME переопределяются).
## img_build в зависимостях гарантирует образ agctl-dsh после docker prune:
## с тёплым кэшем сборка занимает секунды, после полной чистки — минуты.
up: web_build go_build img_build | $(RUNTIME_DIR)
	@if [ -f "$(RUN_UP_PID)" ] && kill -0 "$$(cat $(RUN_UP_PID))" 2>/dev/null; then \
	  echo "agctl уже запущен (pid $$(cat $(RUN_UP_PID))) — make stop"; exit 1; fi
	@AGCTL_HOME="$(RUN_HOME)" nohup ./dist/agctl serve --addr "$(RUN_ADDR)" >>"$(RUN_UP_LOG)" 2>&1 & echo $$! > "$(RUN_UP_PID)"
	@sleep 1
	@if kill -0 "$$(cat $(RUN_UP_PID))" 2>/dev/null; then \
	  echo "✓ agctl: http://$(RUN_ADDR) (дом: $(RUN_HOME))"; \
	  echo "  лог: $(RUN_UP_LOG) · pid: $$(cat $(RUN_UP_PID)) · остановка: make stop"; \
	else \
	  echo "✗ сервер упал при старте — хвост $(RUN_UP_LOG):"; tail -5 "$(RUN_UP_LOG)"; exit 1; fi

## corp: то же, что up, но с корпоративным LiteLLM (ключ — в .env)
corp: web_build go_build img_build | $(RUNTIME_DIR)
	@if [ -f "$(RUN_CORP_PID)" ] && kill -0 "$$(cat $(RUN_CORP_PID))" 2>/dev/null; then \
	  echo "corp-инстанс уже запущен (pid $$(cat $(RUN_CORP_PID))) — make stop"; exit 1; fi
	@if [ -z "$$(printenv AGCTL_LLM_API_KEY)" ]; then \
	  echo "⚠ AGCTL_LLM_API_KEY пуст — скопируй .env.example в .env и впиши ключ."; \
	  echo "  Сервер стартует и без него: у пользователей с личным секретом чат работает."; fi
	@AGCTL_HOME="$(RUN_HOME)" nohup ./dist/agctl serve --addr "$(RUN_ADDR)" \
	  --llm-base-url "$(CORP_LLM_BASE_URL)" \
	  --llm-models "$(CORP_LLM_MODELS)" \
	  --llm-model "$(CORP_LLM_MODEL)" \
	  >>"$(RUN_CORP_LOG)" 2>&1 & echo $$! > "$(RUN_CORP_PID)"
	@sleep 1
	@if kill -0 "$$(cat $(RUN_CORP_PID))" 2>/dev/null; then \
	  echo "✓ agctl (corp: $(CORP_LLM_BASE_URL)): http://$(RUN_ADDR)"; \
	  echo "  лог: $(RUN_CORP_LOG) · остановка: make stop"; \
	else \
	  echo "✗ сервер упал при старте — хвост $(RUN_CORP_LOG):"; tail -5 "$(RUN_CORP_LOG)"; exit 1; fi

## stop: аккуратный останов инстансов, поднятых make (SIGTERM + ожидание)
stop:
	@found=0; \
	for f in $(RUNTIME_DIR)/*.pid; do \
	  [ -f "$$f" ] || continue; found=1; \
	  pid=$$(cat "$$f"); \
	  if kill -0 "$$pid" 2>/dev/null; then \
	    echo "останов $$f: pid $$pid (SIGTERM, ждём до 45с)"; kill -TERM "$$pid"; \
	    i=0; while kill -0 "$$pid" 2>/dev/null && [ $$i -lt 45 ]; do sleep 1; i=$$((i+1)); done; \
	    if kill -0 "$$pid" 2>/dev/null; then \
	      echo "  ⚠ pid $$pid ещё завершается (сервер корректно закрывает чат-контейнеры — проверь позже)"; \
	    else echo "  ✓ остановлен"; rm -f "$$f"; fi; \
	  else echo "  подчищен мёртвый pidfile $$f"; rm -f "$$f"; fi; \
	done; \
	[ "$$found" = 1 ] || echo "инстансов, поднятых make, нет (.runtime/*.pid пусто)"

## down: stop + docker stop всех контейнеров agctl (тома остаются на диске)
down: stop
	@if ! command -v docker >/dev/null 2>&1; then \
	  echo "docker не найден — контейнеры пропущены"; exit 0; fi; \
	names=$$(docker ps -a --format '{{.Names}}' | grep -E '^(agctl-|chat-[0-9a-f]+-u[0-9]+)' || true); \
	if [ -z "$$names" ]; then echo "контейнеров agctl нет"; exit 0; fi; \
	echo "docker stop: $$(echo "$$names" | tr '\n' ' ')"; \
	docker stop $$names

## restart: stop + up
restart: stop up

## logs: хвост лога make-инстанса (другой: make logs LOG=.runtime/corp.log)
logs:
	@log=$(LOG); \
	if [ -z "$$log" ]; then log=$$(ls -t $(RUNTIME_DIR)/*.log 2>/dev/null | head -1); fi; \
	if [ -n "$$log" ]; then echo "хвост $$log (Ctrl+C — выход)"; tail -n 60 -f "$$log"; \
	else echo "логов нет — сначала make up"; fi

## status: что запущено (make-инстансы + контейнеры agctl)
status:
	@seen=0; \
	for f in $(RUNTIME_DIR)/*.pid; do \
	  [ -f "$$f" ] || continue; seen=1; pid=$$(cat "$$f"); \
	  if kill -0 "$$pid" 2>/dev/null; then echo "$$f: работает (pid $$pid)"; \
	  else echo "$$f: не работает (pid $$pid)"; fi; \
	done; \
	[ "$$seen" = 1 ] || echo "make-инстансов нет"; \
	if command -v docker >/dev/null 2>&1; then \
	  docker ps -a --format 'table {{.Names}}\t{{.Status}}' | grep -E 'NAMES|^(agctl-|chat-[0-9a-f]+-u[0-9]+)' || echo "контейнеров agctl нет"; \
	fi
