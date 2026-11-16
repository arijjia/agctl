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

- [x] `bin/release.sh` профилей собирает `./cmd/agctl-guest` под linux и
      кладёт его как `agctl-guest-linux-<arch>`.
- [x] Установка комплекта через `install.sh` (`AGCTL_RELEASE_DIR`) ставит
      `agctl-guest-linux-<arch>` в `~/.local/lib/agctl`.
- [ ] После перехода всех комплектов — убрать поддержку имени
      `agctl-linux-<arch>` из `install.sh`, `install.ps1`, `findLinuxBinary`.

## Журнал

- 2026-11-16: создано при разделении бинарей.
- 2026-11-16: agctl-profile-utmn: bin/release.sh кладёт dist/agctl-guest-linux-<arch> (падает с подсказкой, если его нет — agctl до разделения), README комплекта; ветка agent/agctl-guest вмержена в main локально (98501e4), не запушена. Проверено: make release_local → release.sh → install.sh (AGCTL_RELEASE_DIR, AGCTL_VERSION) поставил agctl-guest-linux-arm64 (ELF arm64 с bridge/ax-runner/init-chat-home). У agctl-profile-rshb сборки комплекта нет — менять нечего. Открыто: снять совместимость с agctl-linux-<arch> после перехода установок (выпуска нового комплекта utmn).
