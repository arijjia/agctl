# ── go: сборка/тесты бинаря agctl ────────────────────────────────────────────

.PHONY: go_build go_test go_vet go_run go_fmt
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
