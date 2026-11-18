# ── install: свежие agctl и agctl-server в PATH + автодополнение fish ───────
# Ставит то, что собрано из текущего checkout'а:
#   $(INSTALL_DIR)/agctl                            — бинарь разработчика;
#   $(INSTALL_DIR)/agctl-server                     — сервер организации (make up/corp);
#   $(INSTALL_LIB)/agctl-guest-linux-<arch демона>  — бинарь образа
#     agctl-dsh: `agctl dev up` / `agctl-server serve` собирают образ сами,
#     если его нет, и ищут его рядом с бинарём и в ../lib/agctl (findLinuxBinary);
#   ~/.config/fish/completions/agctl{,-server}.fish — если fish установлен.
# Старый agctl в PATH — частая причина «неизвестная команда dev».

INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_LIB ?= $(abspath $(INSTALL_DIR)/../lib/agctl)
FISH_COMPLETIONS ?= $(HOME)/.config/fish/completions

.PHONY: install uninstall

## install: собрать (SPA + бинари + agctl-guest) и поставить agctl и agctl-server в INSTALL_DIR (~/.local/bin) + fish-автодополнение
install: web_build go_build go_linux
	@mkdir -p "$(INSTALL_DIR)" "$(INSTALL_LIB)"
	install -m 0755 dist/agctl "$(INSTALL_DIR)/agctl"
	install -m 0755 dist/agctl-server "$(INSTALL_DIR)/agctl-server"
	install -m 0755 dist/agctl-guest-linux-$(GOARCH_DOCKER) "$(INSTALL_LIB)/agctl-guest-linux-$(GOARCH_DOCKER)"
	rm -f "$(INSTALL_LIB)/agctl-linux-arm64" "$(INSTALL_LIB)/agctl-linux-amd64"
	@if command -v fish >/dev/null 2>&1; then \
	  mkdir -p "$(FISH_COMPLETIONS)" && \
	  "$(INSTALL_DIR)/agctl" completion fish > "$(FISH_COMPLETIONS)/agctl.fish" && \
	  "$(INSTALL_DIR)/agctl-server" completion fish > "$(FISH_COMPLETIONS)/agctl-server.fish" && \
	  echo "✓ fish: $(FISH_COMPLETIONS)/agctl.fish, agctl-server.fish"; fi
	@echo "✓ $$("$(INSTALL_DIR)/agctl" version) → $(INSTALL_DIR)/agctl"
	@w=$$(command -v agctl 2>/dev/null); if [ -n "$$w" ] && [ "$$w" != "$(INSTALL_DIR)/agctl" ]; then \
	  echo "⚠ в PATH первым находится $$w, а не $(INSTALL_DIR)/agctl — уберите старый или поправьте PATH"; fi

## uninstall: убрать agctl, agctl-server, agctl-guest и fish-автодополнение, поставленные make install (данные ~/.agctl и ~/.agctl-dev не трогает)
uninstall:
	rm -f "$(INSTALL_DIR)/agctl" "$(INSTALL_DIR)/agctl-server" "$(FISH_COMPLETIONS)/agctl.fish" "$(FISH_COMPLETIONS)/agctl-server.fish"
	rm -f "$(INSTALL_LIB)/agctl-guest-linux-arm64" "$(INSTALL_LIB)/agctl-guest-linux-amd64" "$(INSTALL_LIB)/agctl-linux-arm64" "$(INSTALL_LIB)/agctl-linux-amd64"
	-rmdir "$(INSTALL_LIB)" 2>/dev/null
