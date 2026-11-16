---
schema: 1.0.0
id: split-binaries-local-server
title: "Раздельные бинари: agctl (разработчик), agctl-server (организация), agctl-guest (в образе)"
status: done
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

- [x] Три точки входа; в графе зависимостей `cmd/agctl` нет `k8s.io` и
      `internal/runtime/axrt`; размер бинаря до/после в журнале.
- [x] Сборка: `make go_build`, `go_linux`, `img_build`, `release_local`,
      `install`, goreleaser, `deploy/serve.Dockerfile`, k8s-цели —
      собирают нужный бинарь; `findLinuxBinary` ищет `agctl-guest-linux-<arch>`.
- [x] `bin/install.sh` / `install.ps1` ставят `agctl` + `agctl-guest`.
- [x] Справка и fish-автодополнение у каждого бинаря свои.
- [x] `go vet`, `go test`, `make web_build`, `make img_build`,
      `bin/e2e-local-mode.sh` зелёные.
- [ ] Профиль-репо (`bin/release.sh` в agctl-profile-*) переведены на новые
      имена артефактов — вынесено в `profile-repos-agctl-guest`.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры.
- 2026-11-16: Сделано: cmd/agctl (dev, backlog), cmd/agctl-server (serve с ServerModules и --runtime docker|k8s|ax, migrate, seed-demo-users, connect, sync, config), cmd/agctl-guest (bridge, ax-runner, init-chat-home — в образе как /usr/local/bin/agctl; init-chat-home нужен initContainer k8srt). Общее — internal/cli (Serve/ServeOptions/Runtime, config.yaml, справка, DSH_HOME). Размеры (-s -w): agctl 62,4→19,4 МБ (нет k8s.io, axrt, gRPC), бинарь образа 61,0→21,8 МБ, agctl-server 61,8 МБ. findLinuxBinary ищет agctl-guest-linux-<arch> (agctl-linux-<arch> — совместимость). Makefile, img/k8s/rel/install.mk, serve.Dockerfile, goreleaser (3 сборки/архива), install.sh (прогнан на локальных архивах: релиз и комплект старого формата), install.ps1 (не прогонялся — нет pwsh), e2e-скрипты сервера → agctl-server, README/docs/карты. Проверки: go vet/test, web_build без дрейфа, img_build (тег split-e2e), e2e-local-mode 42/1 — единственный FAIL «ключ LLM в env» существует и на main (там прогон падал раньше: e2e не задавал ключ модели, лаунчер без него не стартует рабочее место) → карточка local-llm-key-container-env; профиль-репо → profile-repos-agctl-guest.
