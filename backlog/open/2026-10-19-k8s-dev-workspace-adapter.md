---
schema: 1.0.0
id: k8s-dev-workspace-adapter
title: "Kubernetes dev-workspace runtime: PVC-backed Git workspace and gateway Service"
status: open
priority: P2
size: L
area: runtime
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-19
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: Current dev workspace data and routing use Docker HostPort/host bind mounts and are not K8s compatible. Port to Pod+Service+PVC (repo+DSH_HOME), persistent Git bare origin, loopback-independent internal service routing, and admin approval quotas.
