# ── img: сборка образов рабочего места (linux-бинарь agctl-guest + встроенный контекст) ─
# Два семейства образов, каждое под свой режим (карточка guest-image-flavours):
#   локальные (agctl-dsh*)        — guest без тегов: только `bridge` (мост к
#     хосту, локальный режим agctl dev);
#   серверные (agctl-dsh-server*) — guest `-tags server`: `ax-runner` (PID 1
#     песочницы AX) и `init-chat-home` (init-контейнер k8s); рантаймы
#     docker|k8s|ax у agctl-server.
IMG ?= agctl-dsh:0.2.2
# Вариант «без SSH» для лаунчера developer'а (карточка developer-launcher-images):
# тот же образ с ENV AGCTL_NO_SSHD=1 — sshd не поднимается даже с ключом.
IMG_NOSSH ?= agctl-dsh-nossh:0.2.2
# Серверные образы рабочего места (agctl serve; k8s/ax берут --image из них).
IMG_SERVER ?= agctl-dsh-server:0.2.2
IMG_SERVER_NOSSH ?= agctl-dsh-server-nossh:0.2.2
# Вкус «web» (локальный режим, веб-разработка): Playwright + Chromium поверх
# базового. IMG_FLAVORS= (пусто) — не собирать вкусы.
IMG_WEB ?= agctl-dsh-web:0.2.2
IMG_FLAVORS ?= web
IMG_ARCH := $(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || echo aarch64)
IMG_GOARCH := $(subst aarch64,arm64,$(subst x86_64,amd64,$(IMG_ARCH)))

# Надстройка образа профиля организации (images.dockerfile в profile.yaml):
# собирается поверх базовых вариантов как agctl-dsh-<id>:<тег> (локальные) и
# agctl-dsh-server-<id>:<тег> (серверные) — те же имена выводит agctl serve.
PROFILE_DOCKERFILE = $(if $(wildcard profiles/$(PROFILE)/profile.yaml),$(shell sed -n 's/^  dockerfile: *//p' profiles/$(PROFILE)/profile.yaml | head -1))
# Вкусы профиля: каталоги profiles/<id>/image/flavors/<вкус>/Dockerfile
# (FROM ${BASE} = надстройка профиля) → agctl-dsh-<id>-<вкус>:<тег>.
PROFILE_FLAVORS = $(notdir $(patsubst %/,%,$(dir $(wildcard profiles/$(PROFILE)/image/flavors/*/Dockerfile))))
flavor_tag = $(call overlay_tag,$(IMG:%:$(lastword $(subst :, ,$(IMG)))=%))-$1:$(lastword $(subst :, ,$(IMG)))
overlay_tag = $(shell printf '%s' '$1' | sed -E 's|^(.*/)?([^/:]+)(:[^/]*)?$$|\1\2-$(PROFILE)\3|')

# Контекст сборки: встроенный образ + скелет DSH_HOME + guest-бинарь файлом agctl.
# $(1) — каталог контекста, $(2) — guest-бинарь из dist/.
define prepare_image_ctx
	rm -rf $(1) && mkdir -p $(1)
	cp -R internal/assets/image/. $(1)/
	mkdir -p $(1)/skel && cp -R internal/assets/dsh-skel/. $(1)/skel/
	cp $(2) $(1)/agctl
endef

.PHONY: img_build img_build_amd64 img_build_nossh img_build_server img_build_server_amd64 img_profile
# В контекст кладём настоящий скелет DSH_HOME: COPY skel/ в Dockerfile требует
# каталог даже пустым, а git пустые каталоги не хранит — на чистом клоне его
# нет (падало из коробки: "/skel": not found). Скелет нужен entrypoint'у для
# самопосева пустого тома из /opt/dsh-skel; форма src/. dst/ не вкладывает
# dsh-skel внутрь, если .build-img/skel уже создан.
## img_build: образы рабочего места: локальные agctl-dsh, agctl-dsh-nossh, вкус web (IMG_FLAVORS) и серверные agctl-dsh-server, agctl-dsh-server-nossh + надстройка профиля PROFILE
img_build:
	GOOS=linux GOARCH=$(IMG_GOARCH) go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-linux-$(IMG_GOARCH) ./cmd/agctl-guest
	GOOS=linux GOARCH=$(IMG_GOARCH) go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-$(IMG_GOARCH) ./cmd/agctl-guest
	$(call prepare_image_ctx,.build-img,dist/agctl-guest-linux-$(IMG_GOARCH))
	docker build -t $(IMG) .build-img
	docker build --build-arg AGCTL_NO_SSHD=1 -t $(IMG_NOSSH) .build-img
	$(call prepare_image_ctx,.build-img-server,dist/agctl-guest-server-linux-$(IMG_GOARCH))
	docker build --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER) .build-img-server
	docker build --build-arg AGCTL_NO_SSHD=1 --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER_NOSSH) .build-img-server
	@if echo " $(IMG_FLAVORS) " | grep -q ' web '; then \
	  docker build --build-arg BASE=$(IMG) -t $(IMG_WEB) .build-img/flavors/web; fi
	@$(MAKE) --no-print-directory img_profile

# PIP_INDEX_URL — зеркало PyPI (Nexus организации) для стадии wheels надстройки.
PIP_INDEX_ARG = $(if $(PIP_INDEX_URL),--build-arg PIP_INDEX_URL=$(PIP_INDEX_URL))

## img_profile: надстройка образа выбранного профиля (PROFILE=<id>) поверх уже собранных базовых (локальные и серверные)
img_profile:
	@if [ -z "$(PROFILE_DOCKERFILE)" ]; then \
	  echo "образ: профиль $(PROFILE) без надстройки — рабочие места на $(IMG) / $(IMG_SERVER)"; exit 0; fi; \
	ctx=profiles/$(PROFILE)/$$(dirname "$(PROFILE_DOCKERFILE)"); \
	f=profiles/$(PROFILE)/$(PROFILE_DOCKERFILE); \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG) -t $(call overlay_tag,$(IMG)) -f "$$f" "$$ctx" && \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG_NOSSH) -t $(call overlay_tag,$(IMG_NOSSH)) -f "$$f" "$$ctx" && \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG_SERVER) -t $(call overlay_tag,$(IMG_SERVER)) -f "$$f" "$$ctx" && \
	docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG_SERVER_NOSSH) -t $(call overlay_tag,$(IMG_SERVER_NOSSH)) -f "$$f" "$$ctx" && \
	if docker image inspect $(IMG_WEB) >/dev/null 2>&1; then \
	  docker build $(PIP_INDEX_ARG) --build-arg BASE=$(IMG_WEB) -t $(call overlay_tag,$(IMG_WEB)) -f "$$f" "$$ctx"; fi && \
	for fl in $(PROFILE_FLAVORS); do \
	  docker build $(PIP_INDEX_ARG) --build-arg BASE=$(call overlay_tag,$(IMG)) -t "$(call flavor_tag,$$fl)" \
	    -f "profiles/$(PROFILE)/image/flavors/$$fl/Dockerfile" "profiles/$(PROFILE)/image/flavors/$$fl" || exit 1; done && \
	echo "✓ образы профиля $(PROFILE): local $(call overlay_tag,$(IMG)), $(call overlay_tag,$(IMG_NOSSH)); server $(call overlay_tag,$(IMG_SERVER)), $(call overlay_tag,$(IMG_SERVER_NOSSH))$(if $(PROFILE_FLAVORS), + вкусы профиля: $(PROFILE_FLAVORS))"

# img_build_nossh: пересобрать только вариант «без SSH» (контекст уже собран
# img_build; бинарь берём из dist).
## img_build_nossh: пересобрать только agctl-dsh-nossh из уже готового .build-img
img_build_nossh:
	docker build --build-arg AGCTL_NO_SSHD=1 -t $(IMG_NOSSH) .build-img

## img_build_server: пересобрать серверные agctl-dsh-server(-nossh): guest-бинарь `-tags server` (ax-runner, init-chat-home)
img_build_server:
	GOOS=linux GOARCH=$(IMG_GOARCH) go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-$(IMG_GOARCH) ./cmd/agctl-guest
	$(call prepare_image_ctx,.build-img-server,dist/agctl-guest-server-linux-$(IMG_GOARCH))
	docker build --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER) .build-img-server
	docker build --build-arg AGCTL_NO_SSHD=1 --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER_NOSSH) .build-img-server

## img_build_amd64: agctl-dsh и agctl-dsh-server под linux/amd64 (например, с Apple Silicon для x86-сервера)
img_build_amd64:
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-linux-amd64 ./cmd/agctl-guest
	GOOS=linux GOARCH=amd64 go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-amd64 ./cmd/agctl-guest
	$(call prepare_image_ctx,.build-img,dist/agctl-guest-linux-amd64)
	docker build --platform linux/amd64 -t $(IMG) .build-img
	$(call prepare_image_ctx,.build-img-server,dist/agctl-guest-server-linux-amd64)
	docker build --platform linux/amd64 --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER) .build-img-server

## img_build_server_amd64: пересобрать только серверные agctl-dsh-server(-nossh) под linux/amd64
img_build_server_amd64:
	GOOS=linux GOARCH=amd64 go build -tags server -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-guest-server-linux-amd64 ./cmd/agctl-guest
	$(call prepare_image_ctx,.build-img-server,dist/agctl-guest-server-linux-amd64)
	docker build --platform linux/amd64 --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER) .build-img-server
	docker build --platform linux/amd64 --build-arg AGCTL_NO_SSHD=1 --build-arg GUEST_FLAVOR=server -t $(IMG_SERVER_NOSSH) .build-img-server