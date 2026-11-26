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
#   ~/.config/fish/completions/agctl{,-server}.fish — если fish установлен.
# В PATH должен быть $(AGCTL_ROOT)/bin. Старый agctl в PATH (например прежний
# ~/.local/bin/agctl) — частая причина «неизвестная команда dev».

AGCTL_ROOT ?= $(HOME)/.agctl
INSTALL_DIR := $(AGCTL_ROOT)/bin
INSTALL_GUEST := $(INSTALL_DIR)/guest
FISH_COMPLETIONS ?= $(HOME)/.config/fish/completions

.PHONY: install uninstall

## install: собрать (SPA + бинари + оба agctl-guest) и поставить под корень AGCTL_ROOT (~/.agctl): bin/agctl, bin/agctl-server, bin/guest/ + fish-автодополнение
install: web_build go_build go_linux go_linux_server
	@mkdir -p "$(INSTALL_DIR)" "$(INSTALL_GUEST)"
	install -m 0755 dist/agctl "$(INSTALL_DIR)/agctl"
	install -m 0755 dist/agctl-server "$(INSTALL_DIR)/agctl-server"
	install -m 0755 dist/agctl-guest-linux-$(GOARCH_DOCKER) "$(INSTALL_GUEST)/agctl-guest-linux-$(GOARCH_DOCKER)"
	install -m 0755 dist/agctl-guest-server-linux-$(GOARCH_DOCKER) "$(INSTALL_GUEST)/agctl-guest-server-linux-$(GOARCH_DOCKER)"
	@if command -v fish >/dev/null 2>&1; then \
	  mkdir -p "$(FISH_COMPLETIONS)" && \
	  "$(INSTALL_DIR)/agctl" completion fish > "$(FISH_COMPLETIONS)/agctl.fish" && \
	  "$(INSTALL_DIR)/agctl-server" completion fish > "$(FISH_COMPLETIONS)/agctl-server.fish" && \
	  echo "✓ fish: $(FISH_COMPLETIONS)/agctl.fish, agctl-server.fish"; fi
	@echo "✓ $$("$(INSTALL_DIR)/agctl" version) → $(INSTALL_DIR)/agctl"
	@case ":$$PATH:" in *":$(INSTALL_DIR):"*) ;; *) \
	  echo "ℹ $(INSTALL_DIR) нет в PATH — добавьте: export PATH=\"$(INSTALL_DIR):\$$PATH\"  (fish: fish_add_path $(INSTALL_DIR))";; esac
	@w=$$(command -v agctl 2>/dev/null); if [ -n "$$w" ] && [ "$$w" != "$(INSTALL_DIR)/agctl" ]; then \
	  echo "⚠ в PATH первым находится $$w, а не $(INSTALL_DIR)/agctl — уберите старый или поправьте PATH"; fi

## uninstall: убрать бинари и guest, поставленные make install, и fish-автодополнение (профили и дома под AGCTL_ROOT не трогает)
uninstall:
	rm -f "$(INSTALL_DIR)/agctl" "$(INSTALL_DIR)/agctl-server" "$(FISH_COMPLETIONS)/agctl.fish" "$(FISH_COMPLETIONS)/agctl-server.fish"
	rm -f "$(INSTALL_GUEST)/agctl-guest-linux-arm64" "$(INSTALL_GUEST)/agctl-guest-linux-amd64" "$(INSTALL_GUEST)/agctl-guest-server-linux-arm64" "$(INSTALL_GUEST)/agctl-guest-server-linux-amd64"
	-rmdir "$(INSTALL_GUEST)" 2>/dev/null
