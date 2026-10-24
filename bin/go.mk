# ── go: сборка/тесты бинаря agctl ────────────────────────────────────────────

# Архитектура демона docker (на той же машине совпадает с хостом); fallback —
# архитектура go. Под эту архитектуру собирается linux-бинарь для образа.
GOARCH_DOCKER := $(subst aarch64,arm64,$(subst x86_64,amd64,$(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || go env GOARCH)))

.PHONY: go_build go_test go_vet go_run go_fmt go_linux
go_build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl ./cmd/agctl

go_test:
	go test ./...

go_vet:
	go vet ./...

go_run: go_build
	./dist/agctl serve

go_fmt:
	gofmt -w cmd internal

## go_linux: linux-сборка бинаря для образа (нужна up/corp при первом старте)
go_linux:
	GOOS=linux GOARCH=$(GOARCH_DOCKER) go build -trimpath -o dist/agctl-linux-$(GOARCH_DOCKER) ./cmd/agctl
