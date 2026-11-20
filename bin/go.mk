# ── go: сборка/тесты бинарей agctl, agctl-server, agctl-guest ───────────────

# Архитектура демона docker (на той же машине совпадает с хостом); fallback —
# архитектура go. Под эту архитектуру собирается agctl-guest для образа.
GOARCH_DOCKER := $(subst aarch64,arm64,$(subst x86_64,amd64,$(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || go env GOARCH)))

.PHONY: go_build go_test go_test_all go_vet go_vet_all go_run go_fmt go_linux go_linux_server
## go_build: бинари под эту машину → dist/agctl (разработчик) и dist/agctl-server (организация); SPA встраивается из web/dist — сначала web_build
go_build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl ./cmd/agctl
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-server ./cmd/agctl-server

## go_test: go test ./... (guest собирается в локальном варианте)
go_test:
	go test ./...

## go_test_all: go test ./... в обоих вариантах guest (без тегов и -tags server)
go_test_all: go_test
	go test -tags server ./...

## go_vet: go vet ./... (guest без тегов)
go_vet:
	go vet ./...

## go_vet_all: go vet ./... в обоих вариантах guest
go_vet_all: go_vet
	go vet -tags server ./...

## go_run: go_build + ./dist/agctl-server serve на переднем плане
go_run: go_build
	./dist/agctl-server serve

## go_fmt: gofmt -w cmd internal
go_fmt:
	gofmt -w cmd internal

## go_linux: agctl-guest локальный вариант (bridge) под linux — бинарь образов agctl-dsh* (нужен agctl dev при первом старте)
go_linux:
	GOOS=linux GOARCH=$(GOARCH_DOCKER) go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-linux-$(GOARCH_DOCKER) ./cmd/agctl-guest

## go_linux_server: agctl-guest серверный вариант (ax-runner, init-chat-home) под linux — бинарь образов agctl-dsh-server* (нужен agctl serve при первом старте)
go_linux_server:
	GOOS=linux GOARCH=$(GOARCH_DOCKER) go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-$(GOARCH_DOCKER) ./cmd/agctl-guest
