# agctl — платформа агентских рабочих мест (один Go-бинарь).
VERSION ?= 0.1.0
LDFLAGS := -s -w -X main.version=$(VERSION)

include bin/*.mk

.PHONY: all build test
all: go_build web_build img_build
build: go_build
test: go_test
