# ── web: React SPA (vite) → web/dist → go:embed ─────────────────────────────

.PHONY: web_install web_build web_dev
## web_install: npm install для SPA (web/)
web_install:
	cd web && npm install

## web_build: собрать React SPA → web/dist (встраивается в бинарь через go:embed)
web_build: web_install
	cd web && npm run build

## web_dev: vite dev-сервер SPA (горячая перезагрузка)
web_dev:
	cd web && npm run dev
