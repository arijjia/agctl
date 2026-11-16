---
schema: 1.0.0
id: profile-repos-agctl-guest
title: "Профиль-репо: комплект с agctl-guest-linux-<arch> вместо agctl-linux-<arch>"
status: open
priority: P2
size: S
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-16
tags: ["release", "split-binaries", "profiles"]
depends: ["split-binaries-local-server"]
---
## Описание

После разделения бинарей (`split-binaries-local-server`) linux-бинарь образа
рабочего места — `agctl-guest` (`cmd/agctl-guest`), а не linux-сборка
`agctl`. `bin/release.sh` в репозиториях профилей (`agctl-profile-utmn`,
`agctl-profile-rshb`) кладёт в комплект `agctl-linux-<arch>` из старой
сборки. `install.sh` / `install.ps1` и `findLinuxBinary` пока понимают оба
имени, но новый `agctl` из `./cmd/agctl` команд образа (`bridge`,
`ax-runner`, `init-chat-home`) не содержит — комплект, собранный из
`./cmd/agctl` под linux, сломает мост в контейнере.

## Критерии готовности

- [ ] `bin/release.sh` профилей собирает `./cmd/agctl-guest` под linux и
      кладёт его как `agctl-guest-linux-<arch>`.
- [ ] Установка комплекта через `install.sh` (`AGCTL_RELEASE_DIR`) ставит
      `agctl-guest-linux-<arch>` в `~/.local/lib/agctl`.
- [ ] После перехода всех комплектов — убрать поддержку имени
      `agctl-linux-<arch>` из `install.sh`, `install.ps1`, `findLinuxBinary`.

## Журнал

- 2026-11-16: создано при разделении бинарей.
