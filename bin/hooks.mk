# ── hooks: git pre-commit ────────────────────────────────────────────────────
# Сам хук живёт в .githooks/pre-commit и проверяет только изменённое:
# gofmt + go vet (по .go), node --check (по .js/.mjs), синк плагинов ↔ скелет,
# блок случайного коммита .env. Тяжёлое (race, gitleaks, e2e) — в CI.

.PHONY: hooks

## hooks: установить pre-commit hook (core.hooksPath=.githooks)
hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "✓ pre-commit активен: .githooks/pre-commit (gofmt · go vet · node --check · синк плагинов · блок .env)"
