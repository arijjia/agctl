# ── profile: профиль организации (данные отдельно от ядра) ──────────────────
# Профиль — отдельное репо-данные (profile.yaml + assets/logo.png), которые
# подключаются в ./profiles/<имя>. Ядро agctl организаций не содержит
# (guard-тест TestCoreIsOrganizationFree); любая организация подключается
# своим профилем без пересборки бинаря.

PROFILES_DIR ?= profiles
# Локальный сиблинг для пилота; для CI/релиза задай PROFILE_REPO=<git-URL>
# (можно с pinned-версией: <url>#refs/tags/v1 резолвится git clone'ом ветки).
PROFILE_REPO ?= ../agctl-profile-utmn
# Имя каталога профиля: по умолчанию — из имени репо/каталога без префикса
# agctl-profile- (agctl-profile-rshb → rshb); явно — PROFILE_NAME=<id>.
PROFILE_NAME ?= $(patsubst agctl-profile-%,%,$(basename $(notdir $(patsubst %/,%,$(PROFILE_REPO)))))

.PHONY: profile-fetch

## profile-fetch: подтянуть профиль организации в ./profiles/<PROFILE_NAME> (локальный каталог или git-URL PROFILE_REPO)
profile-fetch:
	@test -n "$(PROFILE_NAME)" || { echo "✗ не удалось вывести имя профиля из $(PROFILE_REPO) — задай PROFILE_NAME=<id>"; exit 1; }
	@if [ -f "$(PROFILE_REPO)/profile.yaml" ]; then \
	  mkdir -p "$(PROFILES_DIR)/$(PROFILE_NAME)" && cp -R "$(PROFILE_REPO)/." "$(PROFILES_DIR)/$(PROFILE_NAME)/" 2>/dev/null; \
	  rm -rf "$(PROFILES_DIR)/$(PROFILE_NAME)/.git" && \
	  echo "✓ профиль скопирован из $(PROFILE_REPO) в $(PROFILES_DIR)/$(PROFILE_NAME) (.git источника не тащим)"; \
	elif echo "$(PROFILE_REPO)" | grep -qE '^(https?|git|ssh)://|^git@'; then \
	  git clone --depth 1 "$(PROFILE_REPO)" "$(PROFILES_DIR)/$(PROFILE_NAME)" && \
	  echo "✓ профиль склонирован из $(PROFILE_REPO) в $(PROFILES_DIR)/$(PROFILE_NAME)"; \
	else \
	  echo "✗ профиль не найден: $(PROFILE_REPO) (укажи PROFILE_REPO=… — путь или git-URL)"; exit 1; fi
	@test -f "$(PROFILES_DIR)/$(PROFILE_NAME)/profile.yaml" && echo "  профиль: $(PROFILES_DIR)/$(PROFILE_NAME)/profile.yaml (make up PROFILE=$(PROFILE_NAME))"

## profiles: какие профили организаций доступны для запуска (make up PROFILE=<id>)
.PHONY: profiles
profiles:
	@echo "Профили (make up PROFILE=<id>; выбор по умолчанию сейчас: $(PROFILE)):"
	@echo "  agctl  — нейтральный, встроен в бинарь (без организации и LLM-дефолтов)"
	@for f in $(PROFILES_DIR)/*/profile.yaml; do \
	  [ -f "$$f" ] || continue; \
	  id=$$(basename $$(dirname "$$f")); \
	  name=$$(sed -n 's/^  name: *//p' "$$f" | head -1); \
	  tag=$$(sed -n 's/^  tagline: *//p' "$$f" | head -1); \
	  echo "  $$id  — $$name · $$tag"; \
	done
	@[ -n "$(PROFILE_DIRS)" ] || echo "  (профилей организаций нет — make profile-fetch PROFILE_REPO=<git-url или путь>)"
