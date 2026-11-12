# Kubernetes chat runtime — prototype

`agctl serve --runtime k8s` включает Kubernetes-адаптер чата
(`internal/runtime/k8srt`, карточка `k8s-chat-runtime-adapter`). Docker
остаётся default и production target; всё ниже — prototype, живой cluster
smoke не прогнан и **production K8s не заявляется**.

## Что делает

- Per-user Pod + ClusterIP Service + PVC: персистентные имена (PVC/Secret)
  ключуются **StorageID** (`data-<storage>`), compute (Pod/Service/NetworkPolicy)
  — **RuntimeID** (`chat-<runtime>`). Новый RuntimeID с тем же StorageID
  переиспользует том и секрет (регрессия fake-client тестов).
- Init-container сеет DSH_HOME (`agctl init-chat-home`), non-root 1000,
  seccomp `RuntimeDefault`, service account token не монтируется,
  requests/limits заданы, `RuntimeClass` — opt-in.
- `EnsureChat` дожидается Pod Ready (бюджет 120s, `K8sReadinessWait` в
  `server.Config`); Endpoint — ClusterIP DNS воркспейс-гвея agctl.
- `StopChat` удаляет Pod/Service/NetworkPolicy и сохраняет PVC/Secret
  (scale-to-zero: вычисления спят, том пользователя живёт).
- Namespace — storage scope одной установки agctl: выделенный namespace на
  инсталляцию обязателен.

## Как включить

```sh
agctl serve --runtime k8s \
  --k8s-namespace agctl \
  --image registry.example.invalid/agctl-dsh:0.2.2 \
  [--k8s-kubeconfig /path/to/kubeconfig]   # пусто = in-cluster credentials
```

Образ должен быть в реестре, достижимом кластером (локальные Docker-теги не
подходят). env-эквиваленты: `AGCTL_RUNTIME`, `AGCTL_K8S_NAMESPACE`,
`AGCTL_K8S_KUBECONFIG`; `runtime:` в `~/.config/agctl/config.yaml`.

## Ограничения prototype (честно)

- Dev-воркспейсы (`RuntimeIface`), лаунчер и control-plane остаются на Docker:
  их host-path контракт в k8srt не перенесён.
- Live cluster smoke (admission, image pull, init-container, PVC attach,
  readiness, policy, stop) не прогнан — только fake-client тесты, которые не
  считаются доказательством Kubernetes deployment.
- NetworkPolicy: ingress — same-namespace на relay-порту; **egress открыт**
  (egress-allowlist Docker-карточки `egress-allowlist` в k8s не перенесён).

## Требования к кластеру при production-переезде (проверить живьём)

- **RBAC least-privilege**: отдельный ServiceAccount для agctl с правами только
  на pods/services/pods|services|networkingpolicies/secrets/persistentvolumeclaims
  в своём namespace (`get/list/create/delete/update`), без кластерных ролей;
  чат-поды — без SA-токена (уже в прототипе).
- **Secret encryption-at-rest**: ключи LLM лежат в k8s Secret
  (`SecretKeyRef`-доставка в pod, не env в манифесте); включить etcd-шифрование
  `secrets` на кластере (EncryptionConfiguration) и проверить живьём.
- **PVC retention/backup**: PVC переживает StopChat и удаление воркспейса не
  делает (владелец решает явно); для production — storage-класс со снапшотами
  и документированный retention (пользовательские воркспейсы = пользовательские
  данные, бэкап = часть контура платформы).
- **Egress policy**: target-CNI validation (какой CNI исполняет
  NetworkPolicy) перед production; текущий манифест не ограничивает egress.
- **Live smoke чек-лист**: kind/kubectl apply манифестов провайдера →
  admission без warning'ей → pull образа → init-container сеет DSH_HOME →
  PVC attach → readiness → gateway-трафик через Service DNS → StopChat удаляет
  compute и сохраняет PVC → повторный EnsureChat с новым RuntimeID.
