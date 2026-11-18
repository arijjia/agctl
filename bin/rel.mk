# ── rel: локальная кросс-сборка (проверка запуска на 3 ОС) ──────────────────

.PHONY: release_local release_clean
release_clean:
	rm -rf dist

## release_local: agctl — darwin/linux/windows, agctl-server — darwin/linux, agctl-guest — linux (локальный + серверный варианты); × arm64/amd64 в dist/
release_local:
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl ./cmd/agctl
	@set -e; for arch in arm64 amd64; do \
	  for os in darwin linux windows; do \
	    if [ "$$os" = "windows" ]; then ext=.exe; else ext=; fi; \
	    GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-$$os-$$arch$$ext ./cmd/agctl; \
	  done; \
	  for os in darwin linux; do \
	    GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-server-$$os-$$arch ./cmd/agctl-server; \
	  done; \
	  GOOS=linux GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-linux-$$arch ./cmd/agctl-guest; \
	  GOOS=linux GOARCH=$$arch go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-$$arch ./cmd/agctl-guest; \
	done
	@ls -la dist/
