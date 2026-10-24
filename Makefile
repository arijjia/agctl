# agctl — платформа агентских рабочих мест (один Go-бинарь).
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

# Секреты и переопределения — в .env (шаблон: .env.example). .env не коммитится,
# его переменные экспортируются во все цели (serve читает AGCTL_LLM_* сам:
# cmd/agctl/main.go).
-include .env
export

# Общий каталог runtime-артефактов make (pid/логи инстансов up/corp/demo).
RUNTIME_DIR := .runtime

include bin/*.mk

.DEFAULT_GOAL := help
.PHONY: all build test help
all: go_build web_build img_build
build: go_build
test: go_test

## help: список целей
help:
	@echo "agctl — цели make (переопределения: make up RUN_ADDR=… RUN_HOME=…):"; \
	grep -hE '^## ' bin/*.mk Makefile 2>/dev/null | sed 's/^## /  make /'
