# ── img: сборка образа agctl-dsh (linux-бинарь agctl + встроенный контекст) ─
IMG ?= agctl-dsh:0.2.2
IMG_ARCH := $(shell docker version --format '{{.Server.Arch}}' 2>/dev/null || echo aarch64)
IMG_GOARCH := $(subst aarch64,arm64,$(subst x86_64,amd64,$(IMG_ARCH)))

.PHONY: img_build img_build_amd64
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

img_build_amd64:
	GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/agctl-linux-amd64 ./cmd/agctl
	rm -rf .build-img && mkdir -p .build-img
	cp -R internal/assets/image/. .build-img/
	mkdir -p .build-img/skel && cp -R internal/assets/dsh-skel/. .build-img/skel/
	cp dist/agctl-linux-amd64 .build-img/agctl
	docker build --platform linux/amd64 -t $(IMG) .build-img
