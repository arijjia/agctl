# ── go: сборка/тесты бинарей agctl, agctl-server, agctl-guest ───────────────

# Архитектура демона docker (на той же машине совпадает с хостом); fallback —
# архитектура go. Под эту архитектуру собирается agctl-guest для образа.
GOARCH_DOCKER := $(subst aarch64,arm64,$(subst x86_64,amd64,$(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || go env GOARCH)))

.PHONY: go_build go_test go_vet go_run go_fmt go_linux
## go_build: бинари под эту машину → dist/agctl (разработчик) и dist/agctl-server (организация); SPA встраивается из web/dist — сначала web_build
go_build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl ./cmd/agctl
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-server ./cmd/agctl-server

## go_test: go test ./...
go_test:
	go test ./...

## go_vet: go vet ./...
go_vet:
	go vet ./...

## go_run: go_build + ./dist/agctl-server serve на переднем плане
go_run: go_build
	./dist/agctl-server serve

## go_fmt: gofmt -w cmd internal
go_fmt:
	gofmt -w cmd internal

## go_linux: agctl-guest под linux — бинарь образа рабочего места (нужен dev up/up/corp при первом старте)
go_linux:
	GOOS=linux GOARCH=$(GOARCH_DOCKER) go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-linux-$(GOARCH_DOCKER) ./cmd/agctl-guest
