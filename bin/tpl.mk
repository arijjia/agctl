# ── tpl: шаблон 1c-vibe (синк с репо-донором) ────────────────────────────────
VIBE_REPO ?= ../1c-vibe

.PHONY: tpl_check_sync
## tpl_check_sync: сверить шаблон 1c-vibe с checkout репо (перед релизом бинаря)
tpl_check_sync:
	bin/check-1c-vibe-sync.sh $(VIBE_REPO)
