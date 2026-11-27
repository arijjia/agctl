# ── install: свежие agctl и agctl-server под корень ~/.agctl + fish ─────────
# Ставит то, что собрано из текущего checkout'а, в ту же раскладку, что и
# установщики релиза (internal/localmode/layout.go):
#   $(AGCTL_ROOT)/bin/agctl                              — бинарь разработчика;
#   $(AGCTL_ROOT)/bin/agctl-server                       — сервер организации (make up/corp);
#   $(AGCTL_ROOT)/bin/guest/agctl-guest-linux-<arch>     — guest локального режима
#     (bridge) для образов agctl-dsh*: `agctl dev` собирает образ сам, если
#     его нет, и берёт бинарь отсюда;
#   $(AGCTL_ROOT)/bin/guest/agctl-guest-server-linux-<arch> — guest серверного
#     режима (ax-runner, init-chat-home) для образов agctl-dsh-server*.
#   Глобально по cli-std — make install link: относительные симлинки
#   ~/.local/bin/agctl{,-server} → $(AGCTL_ROOT)/bin и
#   ~/.config/fish/completions/agctl{,-server}.fish → completions/ репо
#   (комплишены коммитятся в репо: make completions; тест следит за дрейфом).

AGCTL_ROOT ?= $(HOME)/.agctl
INSTALL_DIR := $(AGCTL_ROOT)/bin
INSTALL_GUEST := $(INSTALL_DIR)/guest
FISH_COMPLETIONS ?= $(HOME)/.config/fish/completions
LOCAL_BIN ?= $(HOME)/.local/bin
relpath = $(shell python3 -c 'import os,sys;print(os.path.relpath(sys.argv[1],sys.argv[2]))' "$(1)" "$(2)")
realpath_py = python3 -c 'import os,sys;print(os.path.realpath(sys.argv[1]))'
# Голый export в Makefile отдаёт рецептам все переменные; функцию relpath
# make тогда раскрывает без аргументов на каждом рецепте — python падал с
# «no path specified». Функции в окружение не нужны.
unexport relpath realpath_py

.PHONY: install uninstall completions link

## install: собрать (SPA + бинари + оба agctl-guest) и поставить под корень AGCTL_ROOT (~/.agctl): bin/agctl, bin/agctl-server, bin/guest/ (в PATH и fish — make link)
install: web_build go_build go_linux go_linux_server
	@mkdir -p "$(INSTALL_DIR)" "$(INSTALL_GUEST)"
	install -m 0755 dist/agctl "$(INSTALL_DIR)/agctl"
	install -m 0755 dist/agctl-server "$(INSTALL_DIR)/agctl-server"
	install -m 0755 dist/agctl-guest-linux-$(GOARCH_DOCKER) "$(INSTALL_GUEST)/agctl-guest-linux-$(GOARCH_DOCKER)"
	install -m 0755 dist/agctl-guest-server-linux-$(GOARCH_DOCKER) "$(INSTALL_GUEST)/agctl-guest-server-linux-$(GOARCH_DOCKER)"
	@echo "✓ $$("$(INSTALL_DIR)/agctl" version) → $(INSTALL_DIR)/agctl"
	@w=$$(command -v agctl 2>/dev/null); r=$$($(realpath_py) "$$w" 2>/dev/null); \
	  if [ -z "$$w" ]; then echo "ℹ agctl нет в PATH — make link (симлинк в ~/.local/bin) или fish_add_path $(INSTALL_DIR)"; \
	  elif [ "$$r" != "$$($(realpath_py) "$(INSTALL_DIR)/agctl")" ] && [ "$$r" != "$$($(realpath_py) "$(CURDIR)/dist/agctl")" ]; then \
	  echo "⚠ в PATH первым находится $$w, а не эта сборка — make link или поправьте PATH"; fi

## completions: перегенерировать fish-комплишены в completions/ (коммитятся; на них смотрят симлинки make link)
completions:
	go run ./cmd/agctl completion fish > completions/agctl.fish
	go run ./cmd/agctl-server completion fish > completions/agctl-server.fish

## link: глобально по cli-std — относительные симлинки ~/.local/bin/agctl{,-server} → артефакты репо dist/ (собирает make install; guest-бинари agctl найдёт рядом в dist/ и в $(AGCTL_ROOT)/bin/guest) и ~/.config/fish/completions/agctl{,-server}.fish → completions/ репо
link:
	@test -x "$(CURDIR)/dist/agctl" || { echo "✗ нет dist/agctl — сначала make install"; exit 1; }
	@mkdir -p "$(LOCAL_BIN)" "$(FISH_COMPLETIONS)"
	ln -sfn "$(call relpath,$(CURDIR)/dist/agctl,$(LOCAL_BIN))" "$(LOCAL_BIN)/agctl"
	ln -sfn "$(call relpath,$(CURDIR)/dist/agctl-server,$(LOCAL_BIN))" "$(LOCAL_BIN)/agctl-server"
	ln -sfn "$(call relpath,$(CURDIR)/completions/agctl.fish,$(FISH_COMPLETIONS))" "$(FISH_COMPLETIONS)/agctl.fish"
	ln -sfn "$(call relpath,$(CURDIR)/completions/agctl-server.fish,$(FISH_COMPLETIONS))" "$(FISH_COMPLETIONS)/agctl-server.fish"
	@echo "✓ $(LOCAL_BIN)/agctl → $$(readlink "$(LOCAL_BIN)/agctl"); fish → $$(readlink "$(FISH_COMPLETIONS)/agctl.fish")"
	@echo "  первая установка: симлинки комплишенов закоммитить в git-репо ~/.config (cli-std)"

## uninstall: убрать бинари и guest, поставленные make install, симлинки make link и fish-автодополнение (профили и дома под AGCTL_ROOT не трогает)
uninstall:
	rm -f "$(INSTALL_DIR)/agctl" "$(INSTALL_DIR)/agctl-server" "$(FISH_COMPLETIONS)/agctl.fish" "$(FISH_COMPLETIONS)/agctl-server.fish"
	rm -f "$(LOCAL_BIN)/agctl" "$(LOCAL_BIN)/agctl-server"
	rm -f "$(INSTALL_GUEST)/agctl-guest-linux-arm64" "$(INSTALL_GUEST)/agctl-guest-linux-amd64" "$(INSTALL_GUEST)/agctl-guest-server-linux-arm64" "$(INSTALL_GUEST)/agctl-guest-server-linux-amd64"
	-rmdir "$(INSTALL_GUEST)" 2>/dev/null
