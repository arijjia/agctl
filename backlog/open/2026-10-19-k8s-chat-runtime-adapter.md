---
schema: 1.0.0
id: k8s-chat-runtime-adapter
title: "Kubernetes ChatProvider: per-user Pod + Service + PVC and scale-to-zero"
status: open
priority: P3
size: L
area: runtime
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-20
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

Docker — текущий runnable target владельца; этот карточный этап оставляет K8s
как проверенную compatibility seam и future adapter, а не production runtime.
Prototype `internal/runtime/k8srt` покрывает Chat Pod/Service/PVC, но не выбран
в `cmd serve`; dev-workspaces и control-plane по-прежнему Docker-specific.
Стабильное хранилище должно следовать `StorageID`, compute — `RuntimeID`, а
Kubernetes namespace является storage scope одной установки agctl.

## Критерии готовности

- Явный opt-in runtime selector; Docker остаётся default и Docker e2e не регрессирует.
- K8s Chat runtime готовит DSH_HOME, ждёт readiness/boot-token и корректно
  переживает stop/start: новый RuntimeID с тем же StorageID повторно использует PVC.
- Live kind/cluster smoke проверяет admission, image pull, init-container,
  PVC attach, readiness, policy и остановку; fake-client tests сами по себе
  не считаются доказательством Kubernetes deployment.
- RBAC least-privilege, gateway-specific ingress/egress policy с target-CNI
  validation, Secret encryption-at-rest и PVC retention/backup описаны и проверены.
- До этих критериев docs и CLI честно называют K8s prototype, не K8s-native.

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: Current `runtime.ChatProvider` contract has StorageID, RuntimeID and Endpoint; only Docker adapter exists. Implement Kubernetes adapter with idempotent EnsureChat by stable RuntimeID, one Pod/ClusterIP Service per active user, PVC per StorageID, readiness/boot-token logs, delete Pod on idle while retaining PVC, resource requests/limits and optional RuntimeClass. Add fake-client integration tests and live cluster smoke before calling the platform K8s-native.
- 2026-10-19: Contract audit: ChatProvider/ChatSpec/ChatHandle now separate server gateway from concrete port; Endpoint can be Service DNS, StorageID can key a PVC. Remaining blockers: server.New hardcodes dockerrt, no K8s client/provider implementation or PVC seed/migrations, and current Docker adapter binds host paths/ports.
- 2026-10-19: Prototype implemented in feature branch: internal/runtime/k8srt ensures per-user PVC+ClusterIP Service+Pod (init-container seeds DSH_HOME via agctl init-chat-home; non-root 1000, seccomp runtime-default, no SA token, RuntimeClass opt-in, requests/limits, ingress NetworkPolicy same-namespace on relay port only). StopChat deletes Pod/Service/NetworkPolicy but keeps PVC+Secret. Fake-client tests cover resource shapes, SecretKeyRef-only API key delivery, idempotent ensure and PVC retention. server.NewWithRuntimeProviders allows injecting the provider; cmd serve still defaults to Docker. Live-cluster smoke, RBAC, encryption-at-rest and PVC retention policy remain.
- 2026-10-20: 2026-10-20 review: owner decision is Docker-first; lower priority P3/future. Audit found PVC/Secret names were derived from RuntimeID despite stable StorageID contract. Fixed prototype to key persistent names by StorageID and added fake-client regression: replacement RuntimeID reuses one PVC/Secret while producing a new Pod/Service. Namespace must be dedicated per agctl installation; basic NP is namespace-wide ingress only, egress open, readiness not awaited, no live cluster smoke.
