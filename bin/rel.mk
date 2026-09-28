# ── rel: локальная кросс-сборка (проверка запуска на 3 ОС) ──────────────────

.PHONY: release_local release_clean
release_clean:
	rm -rf dist

## release_local: darwin/linux/windows × arm64/amd64 в dist/
release_local:
	go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl ./cmd/agctl
	@set -e; for os in darwin linux windows; do \
	  for arch in arm64 amd64; do \
	    if [ "$$os" = "windows" ]; then ext=.exe; else ext=; fi; \
	    GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-$$os-$$arch$$ext ./cmd/agctl; \
	  done; \
	done
	@ls -la dist/
