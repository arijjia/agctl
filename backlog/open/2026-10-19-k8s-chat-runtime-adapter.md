---
schema: 1.0.0
id: k8s-chat-runtime-adapter
title: "Kubernetes ChatProvider: per-user Pod + Service + PVC and scale-to-zero"
status: open
priority: P1
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
- 2026-10-19: Current `runtime.ChatProvider` contract has StorageID, RuntimeID and Endpoint; only Docker adapter exists. Implement Kubernetes adapter with idempotent EnsureChat by stable RuntimeID, one Pod/ClusterIP Service per active user, PVC per StorageID, readiness/boot-token logs, delete Pod on idle while retaining PVC, resource requests/limits and optional RuntimeClass. Add fake-client integration tests and live cluster smoke before calling the platform K8s-native.
- 2026-10-19: Contract audit: ChatProvider/ChatSpec/ChatHandle now separate server gateway from concrete port; Endpoint can be Service DNS, StorageID can key a PVC. Remaining blockers: server.New hardcodes dockerrt, no K8s client/provider implementation or PVC seed/migrations, and current Docker adapter binds host paths/ports.
- 2026-10-19: Prototype implemented in feature branch: internal/runtime/k8srt ensures per-user PVC+ClusterIP Service+Pod (init-container seeds DSH_HOME via agctl init-chat-home; non-root 1000, seccomp runtime-default, no SA token, RuntimeClass opt-in, requests/limits, ingress NetworkPolicy same-namespace on relay port only). StopChat deletes Pod/Service/NetworkPolicy but keeps PVC+Secret. Fake-client tests cover resource shapes, SecretKeyRef-only API key delivery, idempotent ensure and PVC retention. server.NewWithRuntimeProviders allows injecting the provider; cmd serve still defaults to Docker. Live-cluster smoke, RBAC, encryption-at-rest and PVC retention policy remain.
