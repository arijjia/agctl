# ── web: React SPA (vite) → web/dist → go:embed ─────────────────────────────

.PHONY: web_install web_build web_dev
web_install:
	cd web && npm install

web_build: web_install
	cd web && npm run build

web_dev:
	cd web && npm run dev
