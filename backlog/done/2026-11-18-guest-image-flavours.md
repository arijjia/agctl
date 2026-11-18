---
schema: 1.1.0
id: guest-image-flavours
title: "Разделение guest/образа по режимам: bridge-only локальный + серверный флёвор (16→31 МБ)"
status: done
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-11-18
updated: 2026-11-18
tags: ["guest", "image", "flavours"]
depends: []
sessions:
  - session-98f4634c-424c-4a83-a8ae-ae15b09441ab:work
---
## Журнал

- 2026-11-18: создано (`gitctl backlog new`).
- 2026-11-18: Реализация: guest split build-тегом server (local=bridge 10,8 МБ / server=ax-runner+init-chat-home 20,8 МБ, -s -w linux/arm64); образы agctl-dsh-server(-nossh) + GUEST_FLAVOR в Dockerfile; серверные дефолты serve/--image, findServerLinuxBinary; go_linux_server/img_build_server/img_push; rel/install(.sh/.ps1)/goreleaser/k8s.mk; доки README+architecture
- 2026-11-18: Готово и проверено: vet+test в обоих вариантах; img_build собрал оба семейства (0.2.3-flavour-n1); в локальном образе guest local без ax-task-runner, в серверном guest server с обёрткой; e2e-local 44/0, e2e-smoke на agctl-dsh-server 39/0 CHAT=PASS (FAKE_LLM); коммиты c44ecfa (код+упаковка), bdc861c (доки)
- 2026-11-18: done — Разделение выполнено: guest build-тег server; образы agctl-dsh-server(-nossh); серверные дефолты и упаковка; все смоуки зелёные
