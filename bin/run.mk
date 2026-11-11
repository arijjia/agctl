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

# Профиль платформы (--profile): единственный профиль организации из
# ./profiles/<id> (make profile-fetch), если он там один; при нескольких
# (например rshb и utmn) профиль задаётся явно — make up PROFILE=<id>, иначе
# нейтральный agctl. Встроенного профиля организации в бинаре нет.
# Ключ провайдера — в .env (AGCTL_LLM_API_KEY); сами дефолты LLM из профиля,
# точечные переопределения — CORP_LLM_* (пусто = брать из профиля).
PROFILE_DIRS := $(notdir $(patsubst %/,%,$(dir $(wildcard profiles/*/profile.yaml))))
PROFILE ?= $(if $(filter 1,$(words $(PROFILE_DIRS))),$(PROFILE_DIRS),agctl)
# Каталог образов лаунчера developer'а (карточка developer-launcher-images):
# дефолт — пара «с SSH» / «без SSH» (второй собирается img_build).
DEV_IMAGES ?= agctl-dsh:0.2.2,agctl-dsh-nossh:0.2.2
CORP_LLM_BASE_URL ?=
CORP_LLM_MODELS ?=
CORP_LLM_MODEL ?=

# corp-LLM-аргументы: передаются только если переопределены, иначе сервер
# берёт дефолты профиля.
CORP_LLM_ARGS = $(if $(CORP_LLM_BASE_URL),--llm-base-url "$(CORP_LLM_BASE_URL)") \
    $(if $(CORP_LLM_MODELS),--llm-models "$(CORP_LLM_MODELS)") \
    $(if $(CORP_LLM_MODEL),--llm-model "$(CORP_LLM_MODEL)")

# profile_check: подсказка, если выбранный профиль не найден (встроен только agctl)
define profile_check
@echo "профиль: $(PROFILE) (доступны: agctl $(PROFILE_DIRS) — make $1 PROFILE=<id>, список: make profiles)"
@if [ "$(PROFILE)" != "agctl" ] && [ ! -f "profiles/$(PROFILE)/profile.yaml" ]; then \
  echo "⚠ профиль $(PROFILE) не найден в profiles/ (встроен только agctl)."; \
  echo "  Сделай make profile-fetch (PROFILE_REPO=…) или запусти make $1 PROFILE=agctl."; fi
@if [ "$(PROFILE)" = "agctl" ] && [ $(words $(PROFILE_DIRS)) -gt 1 ]; then \
  echo "ℹ в profiles/ несколько профилей ($(PROFILE_DIRS)) — запущен нейтральный agctl; выбери явно: make $1 PROFILE=<id>"; fi
endef

.PHONY: up corp stop down restart logs status demo-users

$(RUNTIME_DIR):
	@mkdir -p $(RUNTIME_DIR)

## up: собрать и поднять платформу в фоне (RUN_ADDR/RUN_HOME переопределяются).
## img_build в зависимостях гарантирует образ agctl-dsh после docker prune:
## с тёплым кэшем сборка занимает секунды, после полной чистки — минуты.
up: web_build go_build img_build | $(RUNTIME_DIR)
	$(call profile_check,up)
	@if [ -z "$(AGCTL_LLM_API_KEY)" ]; then \
	  echo "⚠ AGCTL_LLM_API_KEY пуст: модели будут видны, но запросы основного провайдера не пройдут. Впиши raw-токен в .env (без Bearer/кавычек)."; fi
	@if [ -f "$(RUN_UP_PID)" ] && kill -0 "$$(cat $(RUN_UP_PID))" 2>/dev/null; then \
	  echo "agctl уже запущен (pid $$(cat $(RUN_UP_PID))) — make stop"; exit 1; fi
	@# Порт занят вне-pidfile инстансом → упадём на bind только после сборки.
	@# Лучше поймать сразу и подсказать stop (fallback stop убивает agctl-процесс на порту).
	@port=$$(printf '%s' "$(RUN_ADDR)" | awk -F: '{print $$NF}'); \
	if lsof -nP -ti "tcp:$$port" -sTCP:LISTEN >/dev/null 2>&1; then \
	  echo "✗ порт $(RUN_ADDR) уже занят (инстанс вне make?) — сделай make stop и повтори up"; exit 1; fi
	@AGCTL_HOME="$(RUN_HOME)" nohup ./dist/agctl serve --addr "$(RUN_ADDR)" --profile "$(PROFILE)" --dev-images "$(DEV_IMAGES)" >>"$(RUN_UP_LOG)" 2>&1 & echo $$! > "$(RUN_UP_PID)"
	@sleep 1
	@if kill -0 "$$(cat $(RUN_UP_PID))" 2>/dev/null; then \
	  echo "✓ agctl: http://$(RUN_ADDR) (дом: $(RUN_HOME))"; \
	  echo "  лог: $(RUN_UP_LOG) · pid: $$(cat $(RUN_UP_PID)) · остановка: make stop"; \
	else \
	  echo "✗ сервер упал при старте — хвост $(RUN_UP_LOG):"; tail -5 "$(RUN_UP_LOG)"; exit 1; fi

## demo-users: добавить локальные тестовые роли (после первого реального admin)
demo-users: go_build
	./dist/agctl seed-demo-users --home "$(RUN_HOME)"

## corp: то же, что up, но с корпоративным LiteLLM (ключ — в .env)
corp: web_build go_build img_build | $(RUNTIME_DIR)
	$(call profile_check,corp)
	@if [ -f "$(RUN_CORP_PID)" ] && kill -0 "$$(cat $(RUN_CORP_PID))" 2>/dev/null; then \
	  echo "corp-инстанс уже запущен (pid $$(cat $(RUN_CORP_PID))) — make stop"; exit 1; fi
	@if [ -z "$$(printenv AGCTL_LLM_API_KEY)" ]; then \
	  echo "⚠ AGCTL_LLM_API_KEY пуст — скопируй .env.example в .env и впиши ключи провайдеров."; \
	  echo "  Сервер стартует и без него: у пользователей с личным секретом чат работает."; fi
	@AGCTL_HOME="$(RUN_HOME)" nohup ./dist/agctl serve --addr "$(RUN_ADDR)" --profile "$(PROFILE)" $(CORP_LLM_ARGS) \
	  >>"$(RUN_CORP_LOG)" 2>&1 & echo $$! > "$(RUN_CORP_PID)"
	@sleep 1
	@if kill -0 "$$(cat $(RUN_CORP_PID))" 2>/dev/null; then \
	  echo "✓ agctl (профиль $(PROFILE)): http://$(RUN_ADDR)"; \
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
	@# Fallback: инстанс на RUN_ADDR вне pidfile (запущен вручную или другой
	@# сессией) — иначе stop «ничего не сделал», а порт занят. Убиваем только
	@# процессы, чья командная строка — agctl serve; чужое на порту не трогаем.
	@port=$$(printf '%s' "$(RUN_ADDR)" | awk -F: '{print $$NF}'); \
	pids=$$(lsof -nP -ti "tcp:$$port" -sTCP:LISTEN 2>/dev/null || true); \
	[ -n "$$pids" ] || exit 0; \
	for pid in $$pids; do \
	  cmd=$$(ps -p "$$pid" -o command= 2>/dev/null || true); \
	  case "$$cmd" in \
	    *agctl\ serve*) \
	      echo "останов вне-pidfile инстанс на $(RUN_ADDR): pid $$pid (SIGTERM, ждём до 45с)"; \
	      kill -TERM "$$pid"; \
	      i=0; while kill -0 "$$pid" 2>/dev/null && [ $$i -lt 45 ]; do sleep 1; i=$$((i+1)); done; \
	      if kill -0 "$$pid" 2>/dev/null; then echo "  ⚠ pid $$pid ещё завершается"; \
	      else echo "  ✓ вне-pidfile инстанс остановлен"; fi ;; \
	    *) echo "  ⚠ порт $(RUN_ADDR) держит не agctl (pid $$pid: $$cmd) — не трогаю" ;; \
	  esac; \
	done
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
