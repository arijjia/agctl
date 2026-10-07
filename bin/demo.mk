# ── demo: локальная демонстрация «с ноута» ──────────────────────────────────
HOME_DIR ?= /tmp/agctl-demo

.PHONY: demo demo_fresh demo_clean
## demo: всё собрать и поднять (UI на http://127.0.0.1:8929)
demo: web_build go_build img_build
	AGCTL_HOME=$(HOME_DIR) ./dist/agctl serve

## demo_fresh: с нуля (чистый home, старые данные удаляются)
demo_fresh: demo_clean demo

demo_clean:
	rm -rf $(HOME_DIR)
