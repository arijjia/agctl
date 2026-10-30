# ── profile: профиль организации (данные отдельно от ядра) ──────────────────
# Профиль — отдельное репо-данные (profile.yaml + assets/logo.png), которые
# подключаются в ./profiles/<имя>. Ядро agctl организаций не содержит
# (guard-тест TestCoreIsOrganizationFree); любая организация подключается
# своим профилем без пересборки бинаря.

PROFILES_DIR ?= profiles
# Локальный сиблинг для пилота; для CI/релиза задай PROFILE_REPO=<git-URL>
# (можно с pinned-версией: <url>#refs/tags/v1 резолвится git clone'ом ветки).
PROFILE_REPO ?= ../agctl-profile-utmn

.PHONY: profile-fetch

## profile-fetch: подтянуть профиль организации в ./profiles/utmn (локальный каталог или git-URL)
profile-fetch:
	@if [ -f "$(PROFILE_REPO)/profile.yaml" ]; then \
	  mkdir -p "$(PROFILES_DIR)/utmn" && cp -R "$(PROFILE_REPO)/." "$(PROFILES_DIR)/utmn/" && \
	  echo "✓ профиль скопирован из $(PROFILE_REPO) в $(PROFILES_DIR)/utmn"; \
	elif echo "$(PROFILE_REPO)" | grep -qE '^(https?|git|ssh)://'; then \
	  git clone --depth 1 "$(PROFILE_REPO)" "$(PROFILES_DIR)/utmn" && \
	  echo "✓ профиль склонирован из $(PROFILE_REPO)"; \
	else \
	  echo "✗ профиль не найден: $(PROFILE_REPO) (укажи PROFILE_REPO=… — путь или git-URL)"; exit 1; fi
	@test -f "$(PROFILES_DIR)/utmn/profile.yaml" && echo "  профиль: $(PROFILES_DIR)/utmn/profile.yaml"
