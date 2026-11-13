# ── img: сборка образа agctl-dsh (linux-бинарь agctl + встроенный контекст) ─
IMG ?= agctl-dsh:0.2.2
# Вариант «без SSH» для лаунчера developer'а (карточка developer-launcher-images):
# тот же образ с ENV AGCTL_NO_SSHD=1 — sshd не поднимается даже с ключом.
IMG_NOSSH ?= agctl-dsh-nossh:0.2.2
IMG_ARCH := $(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || echo aarch64)
IMG_GOARCH := $(subst aarch64,arm64,$(subst x86_64,amd64,$(IMG_ARCH)))

# Надстройка образа профиля организации (images.dockerfile в profile.yaml):
# собирается поверх обоих базовых вариантов как agctl-dsh-<id>:<тег> и
# agctl-dsh-<id>-nossh:<тег> — те же имена выводит agctl serve.
PROFILE_DOCKERFILE = $(if $(wildcard profiles/$(PROFILE)/profile.yaml),$(shell sed -n 's/^  dockerfile: *//p' profiles/$(PROFILE)/profile.yaml | head -1))
overlay_tag = $(shell printf '%s' '$1' | sed -E 's|^(.*/)?([^/:]+)(:[^/]*)?$$|\1\2-$(PROFILE)\3|')

.PHONY: img_build img_build_amd64 img_build_nossh img_profile
# В контекст кладём настоящий скелет DSH_HOME: COPY skel/ в Dockerfile требует
# каталог даже пустым, а git пустые каталоги не хранит — на чистом клоне его
# нет (падало из коробки: "/skel": not found). Скелет нужен entrypoint'у для
# самопосева пустого тома из /opt/dsh-skel; форма src/. dst/ не вкладывает
# dsh-skel внутрь, если .build-img/skel уже создан.
img_build:
	GOOS=linux GOARCH=$(IMG_GOARCH) go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-linux-$(IMG_GOARCH) ./cmd/agctl
	rm -rf .build-img && mkdir -p .build-img
	cp -R internal/assets/image/. .build-img/
	mkdir -p .build-img/skel && cp -R internal/assets/dsh-skel/. .build-img/skel/
	cp dist/agctl-linux-$(IMG_GOARCH) .build-img/agctl
	docker build -t $(IMG) .build-img
	docker build --build-arg AGCTL_NO_SSHD=1 -t $(IMG_NOSSH) .build-img
	@$(MAKE) --no-print-directory img_profile

# PIP_INDEX_URL — зеркало PyPI (Nexus организации) для стадии wheels надстройки.
PIP_INDEX_ARG = $(if $(PIP_INDEX_URL),--build-arg PIP_INDEX_URL=$(PIP_INDEX_URL))

## img_profile: надстройка образа выбранного профиля (PROFILE=<id>) поверх уже собранных базовых
img_profile:
	@if [ -z "$(PROFILE_DOCKERFILE)" ]; then \
	  echo "образ: профиль $(PROFILE) без надстройки — рабочие места на $(IMG)"; exit 0; fi; \
	ctx=profiles/$(PROFILE)/$$(dirname "$(PROFILE_DOCKERFILE)"); \
	f=profiles/$(PROFILE)/$(PROFILE_DOCKERFILE); \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG) -t $(call overlay_tag,$(IMG)) -f "$$f" "$$ctx" && \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG_NOSSH) -t $(call overlay_tag,$(IMG_NOSSH)) -f "$$f" "$$ctx" && \
	echo "✓ образы профиля $(PROFILE): $(call overlay_tag,$(IMG)), $(call overlay_tag,$(IMG_NOSSH))"

# img_build_nossh: пересобрать только вариант «без SSH» (контекст уже собран
# img_build; бинарь берём из dist).
img_build_nossh:
	docker build --build-arg AGCTL_NO_SSHD=1 -t $(IMG_NOSSH) .build-img

img_build_amd64:
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-linux-amd64 ./cmd/agctl
	rm -rf .build-img && mkdir -p .build-img
	cp -R internal/assets/image/. .build-img/
	mkdir -p .build-img/skel && cp -R internal/assets/dsh-skel/. .build-img/skel/
	cp dist/agctl-linux-amd64 .build-img/agctl
	docker build --platform linux/amd64 -t $(IMG) .build-img
