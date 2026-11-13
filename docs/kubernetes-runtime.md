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

## Ограничения (честно, live не проверено)

- Dev-воркспейсы и control-plane запускаются на Docker даже при `--runtime k8s`
  (host-path контракт workspace'ов в k8s не перенесён; upstream передаёт
  provider и как RuntimeIface, но живой прогон этого режима не делался).
- Live cluster smoke (admission, image pull, init-container, PVC attach,
  readiness, policy, stop) в рамках карточки не прогнан — docker/kind в
  сессии были недоступны; fake-client тесты доказательством deployment не
  считаются. Чек-лист см. `docs/kubernetes.md` + `bin/e2e-k8s-kind.sh`.

## Требования к кластеру при production-переезде (проверить живьём)

- **RBAC least-privilege**: отдельный ServiceAccount agctl с правами только на
  pods/services/networkpolicies/secrets/pvc в своём namespace, без кластерных
  ролей; чат-поды — без SA-токена.
- **Secret encryption-at-rest**: ключи LLM только через `SecretKeyRef`;
  включить etcd-шифрование `secrets` (EncryptionConfiguration) и проверить.
- **PVC retention/backup**: storage-класс со снапшотами, документированный
  retention (пользовательские воркспейсы = пользовательские данные).
- **Egress policy**: egress подов не ограничен — target-CNI validation и
  egress-политика (эквивалент Docker `egress-allowlist`) до production.
