# ── install: свежий agctl в PATH + автодополнение fish ──────────────────────
# Ставит то, что собрано из текущего checkout'а:
#   $(INSTALL_DIR)/agctl                         — бинарь под эту машину;
#   $(INSTALL_LIB)/agctl-linux-<arch демона>     — linux-сборка для образа
#     agctl-dsh: `agctl dev up` / `agctl serve` собирают образ сами, если его
#     нет, и ищут её рядом с бинарём и в ../lib/agctl (findLinuxBinary);
#   ~/.config/fish/completions/agctl.fish        — если fish установлен.
# Старый agctl в PATH — частая причина «неизвестная команда dev».

INSTALL_DIR ?= $(HOME)/.local/bin
INSTALL_LIB ?= $(abspath $(INSTALL_DIR)/../lib/agctl)
FISH_COMPLETIONS ?= $(HOME)/.config/fish/completions

.PHONY: install uninstall

## install: собрать (SPA + бинарь + linux-сборка) и поставить agctl в INSTALL_DIR (~/.local/bin) + fish-автодополнение
install: web_build go_build go_linux
	@mkdir -p "$(INSTALL_DIR)" "$(INSTALL_LIB)"
	install -m 0755 dist/agctl "$(INSTALL_DIR)/agctl"
	install -m 0755 dist/agctl-linux-$(GOARCH_DOCKER) "$(INSTALL_LIB)/agctl-linux-$(GOARCH_DOCKER)"
	@if command -v fish >/dev/null 2>&1; then \
	  mkdir -p "$(FISH_COMPLETIONS)" && "$(INSTALL_DIR)/agctl" completion fish > "$(FISH_COMPLETIONS)/agctl.fish" && \
	  echo "✓ fish: $(FISH_COMPLETIONS)/agctl.fish"; fi
	@echo "✓ $$("$(INSTALL_DIR)/agctl" version) → $(INSTALL_DIR)/agctl"
	@w=$$(command -v agctl 2>/dev/null); if [ -n "$$w" ] && [ "$$w" != "$(INSTALL_DIR)/agctl" ]; then \
	  echo "⚠ в PATH первым находится $$w, а не $(INSTALL_DIR)/agctl — уберите старый или поправьте PATH"; fi

## uninstall: убрать agctl, linux-сборки и fish-автодополнение, поставленные make install (данные ~/.agctl и ~/.agctl-dev не трогает)
uninstall:
	rm -f "$(INSTALL_DIR)/agctl" "$(FISH_COMPLETIONS)/agctl.fish"
	rm -f "$(INSTALL_LIB)/agctl-linux-arm64" "$(INSTALL_LIB)/agctl-linux-amd64"
	-rmdir "$(INSTALL_LIB)" 2>/dev/null
