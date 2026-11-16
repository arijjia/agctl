---
schema: 1.0.0
id: split-binaries-local-server
title: "Раздельные бинари: agctl (разработчик), agctl-server (организация), agctl-guest (в образе)"
status: open
priority: P2
size: L
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-16
tags: ["architecture", "release", "split-binaries"]
depends: ["server-pluggable-modules"]
---
## Описание

Один бинарь `agctl` (62 МБ) несёт всем разработчикам client-go, gRPC/AX,
админку и серверные команды. Решение: одно ядро, отдельные точки входа.

- `cmd/agctl` — для разработчика: `dev …`, `version`, `completion`,
  `backlog`. Только Docker-рантайм и локальный режим.
- `cmd/agctl-server` — для организации: `serve`, `migrate`,
  `init-chat-home`, `seed-demo-users`, `connect`, `sync`, `config`;
  рантаймы docker | k8s | ax и серверные модули.
- `cmd/agctl-guest` — linux-бинарь внутри образа `agctl-dsh`, ставится как
  `/usr/local/bin/agctl` (контракты `agctl bridge …` и `agctl ax-runner`
  сохраняются): `bridge`, `ax-runner`, `version`.

Общий код CLI (версия, serve) — в `internal/cli`, чтобы `agctl dev serve`
и `agctl-server serve` шли через одну функцию.

## Критерии готовности

- [ ] Три точки входа; в графе зависимостей `cmd/agctl` нет `k8s.io` и
      `internal/runtime/axrt`; размер бинаря до/после в журнале.
- [ ] Сборка: `make go_build`, `go_linux`, `img_build`, `release_local`,
      `install`, goreleaser, `deploy/serve.Dockerfile`, k8s-цели —
      собирают нужный бинарь; `findLinuxBinary` ищет `agctl-guest-linux-<arch>`.
- [ ] `bin/install.sh` / `install.ps1` ставят `agctl` + `agctl-guest`.
- [ ] Справка и fish-автодополнение у каждого бинаря свои.
- [ ] `go vet`, `go test`, `make web_build`, `make img_build`,
      `bin/e2e-local-mode.sh` зелёные.
- [ ] Профиль-репо (`bin/release.sh` в agctl-profile-*) переведены на новые
      имена артефактов — отдельно, в их репозиториях.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры.
