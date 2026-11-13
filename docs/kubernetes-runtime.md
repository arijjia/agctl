# Kubernetes chat runtime — интеграция с upstream (карточка k8s-chat-runtime-adapter)

Upstream `origin/main` (коммит 9583119 и след.) принёс собственную, более
богатую реализацию K8s-бэкенда. По решению владельца (журнал карточки,
2026-11-13) интеграция резолвится **в пользу upstream-версии**; эта ветка
переносит на неё критерии карточки: ожидание Pod Ready (readiness-wait) и
этот документ. Операционная документация upstream — `docs/kubernetes.md`
(деплой, манифесты `deploy/k8s`, kind-смоук `bin/e2e-k8s-kind.sh`); здесь —
дельта и непроверенное-live.

## Селектор runtime (upstream-архитектура)

`agctl serve --runtime docker|k8s|ax` — выбор в `cmd/agctl/runtime_select.go`
(`buildServer`): docker → `server.New`, k8s/ax → `server.NewWithRuntimeProviders`
с адаптером и как ChatProvider, и как legacy `RuntimeIface`. Валидация имени —
`parseRuntimeKind` до конструкторов (fast-fail без Docker-демона). Docker —
default; env `AGCTL_RUNTIME`, `serve.runtime` в конфиге.

## Readiness-wait (критерий карточки, перенесён на upstream API)

`k8srt.Config{WaitReady bool, ReadyTimeout time.Duration}` (дефолт 180s,
флаг `--k8s-ready-timeout`, env `AGCTL_K8S_READY_TIMEOUT`): `EnsureChat` при
`WaitReady` дожидается Pod Ready (`waitReady`), при таймауте роняет запрос,
сохраняя Pod/PVC для диагностики. Fake-client тесты карточки
(`TestEnsureChatWaitsForPodReady`, `TestEnsureChatReadinessTimeout`) перенесены
на WaitReady/ReadyTimeout.

## Что сверх старого прототипа даёт upstream

Per-user Pod + ClusterIP Service; персистентность — PVC на пользователя или
общий RWX-том (`--k8s-shared-claim`, подкаталог `<StorageID>/`, шлюз монтирует
том как `$AGCTL_HOME/chats`); PVC/Secret ключуются StorageID, compute —
RuntimeID (критерий карточки выполнен); SSH developer'а через NodePort;
NetworkPolicy на поды шлюза (`--k8s-gateway-labels`); livenessProbe;
SweepVersionDrift по метке версии; pods/exec.

## Live-верификация (kind, 2026-11-13)

`bin/e2e-k8s-kind.sh` прогнан живьём (kind v0.33 в colima на macOS/arm64,
NodePort обойдён через `BASE_URL` + `kubectl port-forward` — extraPortMappings
в nested docker до хоста не доносятся): admission без warnings, pull образов,
init-container DSH_HOME, PVC attach, readiness (WaitReady), gateway-трафик
через Service DNS, StopChat удаляет compute и сохраняет PVC —
`✓ k8s e2e smoke OK`.

RBAC least-privilege проверен живьём (`kubectl auth can-i` от SA `agctl`):
создание/удаление pods/services/networkpolicies/secrets/pvc в своём
namespace — yes; clusterroles/namespaces и чужой namespace — no.

По ходу прогона починен сам смоук и kind-overlay (см. коммиты ветки):
image-transform матчил переименованное base-имя (ErrImageNeverPull),
`AGCTL_PROFILE_DIR` без ConfigMap профиля ронял serve, kustomize-патч
volumes, cookie/-L в /chat/-цикле, идемпотентный register, роль developer
через role-request для stop-ручки, выбор новейшего чат-пода.

## Ограничения (честно, live не проверено)

- Dev-воркспейсы и control-plane запускаются на Docker даже при `--runtime k8s`
  (host-path контракт workspace'ов в k8s не перенесён; upstream передаёт
  provider и как RuntimeIface, но живой прогон этого режима не делался).
- NetworkPolicy исполняет только CNI с policy-поддержкой; kind (kindnet) её
  НЕ энфорсит — ingress/egress-изоляция на целевом кластере требует
  target-CNI validation.

## Требования к кластеру при production-переезде (проверить живьём)

- **Egress policy**: egress подов не ограничен — target-CNI validation и
  egress-политика (эквивалент Docker `egress-allowlist`) до production.
- **Secret encryption-at-rest**: ключи LLM только через `SecretKeyRef`;
  включить etcd-шифрование `secrets` (EncryptionConfiguration) и проверить
  живьём (в kind не проверяемо осмысленно).
- **PVC backup**: retention подов не удаляет PVC (live ✓ в смоук); для
  production — storage-класс со снапшотами и документированный retention.
