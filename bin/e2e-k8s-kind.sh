#!/usr/bin/env bash
# e2e-k8s-kind.sh — смоук бэкенда k8s в одноразовом kind-кластере:
# собирает образы agctl-serve/agctl-dsh, грузит их в узел kind, применяет
# deploy/k8s/overlays/kind, регистрирует первого пользователя (admin) и
# второго (user), открывает /chat/ от user'а и ждёт, пока шлюз поднимет под
# dsh и ответит чат-шеллом; затем проверяет сон рантайма (POST
# /api/me/runtime/stop) и сохранение PVC. Нужны: docker, kind, kubectl, curl.
#   KEEP=1 — не удалять кластер; KIND_CLUSTER — имя (agctl-e2e).
set -euo pipefail
cd "$(dirname "$0")/.."

KIND_CLUSTER="${KIND_CLUSTER:-agctl-e2e}"
NS=agctl
PORT=30929
TAG=0.2.2
need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ нужен $1"; exit 1; }; }
need docker; need kind; need kubectl; need curl

cleanup() {
  if [ "${KEEP:-0}" != "1" ]; then kind delete cluster --name "$KIND_CLUSTER" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

echo "▶ kind-кластер $KIND_CLUSTER"
if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  cat <<KIND | kind create cluster --name "$KIND_CLUSTER" --config=- --wait 120s
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: ${PORT}
        hostPort: ${PORT}
KIND
fi
kubectl config use-context "kind-${KIND_CLUSTER}" >/dev/null

echo "▶ образы"
ARCH=$(docker version --format '{{.Server.Arch}}' | sed -e 's/aarch64/arm64/' -e 's/x86_64/amd64/')
make img_build IMG="agctl-dsh:${TAG}" IMG_NOSSH="agctl-dsh-nossh:${TAG}" >/dev/null
make img_serve_build K8S_ARCH="$ARCH" IMG_SERVE=agctl-serve:dev >/dev/null
kind load docker-image --name "$KIND_CLUSTER" agctl-serve:dev "agctl-dsh:${TAG}"

echo "▶ деплой"
kubectl apply -k deploy/k8s/overlays/kind
kubectl -n "$NS" rollout status deployment/agctl --timeout=240s
# BASE_URL — обход NodePort (например, kind в colima/nested docker, где
# extraPortMappings до хоста не доносятся): kubectl port-forward + свой адрес.
BASE="${BASE_URL:-http://127.0.0.1:${PORT}}"
for _ in $(seq 1 60); do curl -fsS "$BASE/" >/dev/null 2>&1 && break; sleep 2; done
curl -fsS "$BASE/" | grep -q "__AGCTL_BRAND__" && echo "  ✓ страница входа"

JAR_ADMIN=$(mktemp); JAR_USER=$(mktemp)
register() { # идемпотентно: на существующем кластере register даёт 400 — логинимся
  curl -fsS -c "$1" -H 'Content-Type: application/json' -d "{\"login\":\"$2\",\"password\":\"$3\"}" "$BASE/api/register" >/dev/null 2>&1 ||
  curl -fsS -c "$1" -H 'Content-Type: application/json' -d "{\"login\":\"$2\",\"password\":\"$3\"}" "$BASE/api/login" >/dev/null
}
register "$JAR_ADMIN" e2eadmin 'E2e-admin-pass-1!'
register "$JAR_USER" e2euser 'E2e-user-pass-1!'
echo "  ✓ пользователи зарегистрированы"

echo "▶ /chat/ от user: ожидание пода dsh (до 5 мин)"
ok=0
for _ in $(seq 1 150); do
  # -L + сохранение cookie: первый /chat/ поднимает под и редиректит (303)
  # на себя же; без записи cookie цикл ловит 303 вечно.
  code=$(curl -s -L -c "$JAR_USER" -b "$JAR_USER" -o /tmp/agctl-e2e-chat.html -w '%{http_code}' "$BASE/chat/")
  if [ "$code" = "200" ] && grep -q "__AGCTL_BRAND__" /tmp/agctl-e2e-chat.html; then ok=1; break; fi
  sleep 2
done
[ "$ok" = "1" ] || { echo "✗ чат не поднялся (код $code)"; kubectl -n "$NS" get pod -o wide; kubectl -n "$NS" logs deployment/agctl --tail=50; exit 1; }
POD=$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl-dsh-chat -o jsonpath='{.items[0].metadata.name}')
echo "  ✓ под пользователя: $POD"
kubectl -n "$NS" get pvc -l app.kubernetes.io/name=agctl-dsh-chat | grep -q data- && echo "  ✓ PVC пользователя создан"

echo "▶ сон рантайма"
curl -fsS -X POST -b "$JAR_USER" "$BASE/api/me/runtime/stop" | grep -q sleeping
for _ in $(seq 1 60); do kubectl -n "$NS" get pod "$POD" >/dev/null 2>&1 || break; sleep 2; done
kubectl -n "$NS" get pod "$POD" >/dev/null 2>&1 && { echo "✗ под не удалён после stop"; exit 1; }
kubectl -n "$NS" get pvc -l app.kubernetes.io/name=agctl-dsh-chat | grep -q data- && echo "  ✓ compute удалён, PVC сохранён"

echo "▶ админ видит рантаймы"
curl -fsS -b "$JAR_ADMIN" "$BASE/api/admin/containers" | head -c 300; echo
echo "✓ k8s e2e smoke OK"
