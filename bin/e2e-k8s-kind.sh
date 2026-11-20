#!/usr/bin/env bash
# e2e-k8s-kind.sh — смоук бэкенда k8s в одноразовом kind-кластере:
# собирает образы agctl-serve/agctl-dsh-server, грузит их в узел kind, применяет
# deploy/k8s/overlays/kind, регистрирует первого пользователя (admin) и
# второго (user), открывает /chat/ от user'а и ждёт, пока шлюз поднимет под
# dsh и ответит чат-шеллом; затем проверяет сон рантайма (POST
# /api/me/runtime/stop) и сохранение PVC. Нужны: docker, kind, kubectl, curl.
#   KEEP=1 — не удалять кластер; KIND_CLUSTER — имя (agctl-e2e).
#   EGRESS=1 — смоук opt-in egress-политики чат-подов (карточка
#     k8s-pod-egress-policy): ставит Calico (kindnet не энфорсит
#     NetworkPolicy), поднимает фейковый LLM-хост (headless svc fake-llm),
#     включает --k8s-egress-policy с --llm-base-url на фейк и проверяет:
#     разрешённый хост отвечает, посторонний (шлюз) — блокируется.
set -euo pipefail
cd "$(dirname "$0")/.."

KIND_CLUSTER="${KIND_CLUSTER:-agctl-e2e}"
NS=agctl
PORT=30929
TAG=0.2.2
EGRESS="${EGRESS:-0}"
CALICO_MANIFEST="https://raw.githubusercontent.com/projectcalico/calico/v3.29.2/manifests/calico.yaml"
need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ нужен $1"; exit 1; }; }
need docker; need kind; need kubectl; need curl

PF_PID=""
cleanup() {
  [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null || true
  if [ "${KEEP:-0}" != "1" ]; then kind delete cluster --name "$KIND_CLUSTER" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

echo "▶ kind-кластер $KIND_CLUSTER"
if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  if [ "$EGRESS" = "1" ]; then
    # Calico ставится вместо штатного kindnet (тот не энфорсит NetworkPolicy).
    cat <<KIND | kind create cluster --name "$KIND_CLUSTER" --config=- --wait 120s
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: ${PORT}
        hostPort: ${PORT}
KIND
    echo "▶ Calico (policy-CNI)"
    kubectl apply -f "$CALICO_MANIFEST" >/dev/null
    kubectl -n kube-system rollout status daemonset/calico-node --timeout=420s >/dev/null
    echo "  ✓ calico-node готов"
  else
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
fi
kubectl config use-context "kind-${KIND_CLUSTER}" >/dev/null

echo "▶ образы"
ARCH=$(docker version --format '{{.Server.Arch}}' | sed -e 's/aarch64/arm64/' -e 's/x86_64/amd64/')
make img_build IMG="agctl-dsh:${TAG}" IMG_NOSSH="agctl-dsh-nossh:${TAG}" IMG_SERVER="agctl-dsh-server:${TAG}" IMG_SERVER_NOSSH="agctl-dsh-server-nossh:${TAG}" IMG_FLAVORS= >/dev/null
make img_serve_build K8S_ARCH="$ARCH" IMG_SERVE=agctl-serve:dev >/dev/null
kind load docker-image --name "$KIND_CLUSTER" agctl-serve:dev "agctl-dsh-server:${TAG}"

echo "▶ деплой"
kubectl apply -k deploy/k8s/overlays/kind
if [ "$EGRESS" = "1" ]; then
  # Фейковый LLM-хост: headless svc — DNS шлюза вернёт pod IP, он попадёт в
  # ipBlock egress-политики чата (ClusterIP в ipBlock не работает).
  cat <<FAKE | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Service
metadata:
  name: fake-llm
  namespace: ${NS}
spec:
  clusterIP: None
  selector:
    app: fake-llm
  ports:
    - port: 8000
---
apiVersion: v1
kind: Pod
metadata:
  name: fake-llm
  namespace: ${NS}
  labels:
    app: fake-llm
spec:
  containers:
    - name: httpd
      image: busybox:1.36
      args: ["sh", "-c", "mkdir -p /tmp/www && echo ok > /tmp/www/index.html && exec httpd -f -p 8000 -h /tmp/www"]
      ports:
        - containerPort: 8000
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
        seccompProfile:
          type: RuntimeDefault
FAKE
  kubectl -n "$NS" wait --for=condition=Ready pod/fake-llm --timeout=120s >/dev/null
  # Opt-in egress-политика + LLM-эндпоинт шлюза на фейковый хост (его
  # hostname попадёт в ipBlock через DNS-ресолв шлюза).
  kubectl -n "$NS" set env deployment/agctl AGCTL_K8S_EGRESS_POLICY=1 \
    AGCTL_LLM_BASE_URL=http://fake-llm.${NS}.svc.cluster.local:8000/v1 >/dev/null
fi
kubectl -n "$NS" rollout status deployment/agctl --timeout=240s
# BASE_URL — явный обход NodePort (kind в colima/nested docker, Docker Desktop
# c неработающими host-маппингами и т.п.): kubectl port-forward + свой адрес.
# Без BASE_URL сначала пробуем NodePort (extraPortMappings); если хост не
# получает ответа (известный кейс Docker Desktop↔kind: соединение есть,
# ответ не возвращается) — автоматически падаем на port-forward.
BASE="${BASE_URL:-http://127.0.0.1:${PORT}}"
probe() { curl -fsS --max-time 5 "$1/" >/dev/null 2>&1; }
if [ -z "${BASE_URL:-}" ]; then
  ok=0
  for _ in $(seq 1 15); do if probe "$BASE"; then ok=1; break; fi; sleep 2; done
  if [ "$ok" != "1" ]; then
    echo "  ⚠ NodePort $PORT с хоста не отвечает — fallback на kubectl port-forward"
    PF_PORT=30930
    kubectl -n "$NS" port-forward service/agctl "${PF_PORT}:80" >/dev/null 2>&1 &
    PF_PID=$!
    BASE="http://127.0.0.1:${PF_PORT}"
    for _ in $(seq 1 30); do if probe "$BASE"; then break; fi; sleep 2; done
  fi
fi
for _ in $(seq 1 60); do if probe "$BASE"; then break; fi; sleep 2; done
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
POD=$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl-dsh-chat --sort-by=.metadata.creationTimestamp -o jsonpath='{.items[-1].metadata.name}') # новейший: под только что поднят этим прогоном
echo "  ✓ под пользователя: $POD"
kubectl -n "$NS" get pvc -l app.kubernetes.io/name=agctl-dsh-chat | grep -q data- && echo "  ✓ PVC пользователя создан"

if [ "$EGRESS" = "1" ]; then
  echo "▶ egress-политика чат-пода"
  kubectl -n "$NS" get networkpolicy "netegress-$POD" >/dev/null && echo "  ✓ политика netegress создана"
  # Разрешённый LLM-хост: HTTP-ответ приходит (код неважен — соединение есть).
  set +e
  code=$(kubectl -n "$NS" exec "$POD" -c dsh -- curl -s -o /dev/null -w '%{http_code}' --max-time 8 "http://fake-llm.${NS}.svc.cluster.local:8000/" 2>/dev/null)
  rc=$?
  set -e
  if [ "$rc" = "0" ] && [ "$code" != "000" ] && [ -n "$code" ]; then
    echo "  ✓ разрешённый хост отвечает (HTTP $code)"
  else
    echo "✗ разрешённый хост не отвечает (rc=$rc код=$code)"; exit 1
  fi
  # Посторонний хост: pod IP шлюза — соединение должно повиснуть (timeout).
  GWIP=$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl -o jsonpath='{.items[0].status.podIP}')
  if kubectl -n "$NS" exec "$POD" -c dsh -- curl -s -o /dev/null --max-time 6 "http://${GWIP}:8929/" 2>/dev/null; then
    echo "✗ посторонний хост ($GWIP шлюз) не заблокирован"; exit 1
  fi
  echo "  ✓ посторонний хост (шлюз $GWIP) блокируется"
fi

echo "▶ сон рантайма"
# /api/me/runtime/* требует роль developer: заявка от user + апрув админом.
curl -fsS -b "$JAR_USER" -H 'Content-Type: application/json' -d '{"motivation":"e2e"}' "$BASE/api/role-requests" >/dev/null 2>&1 || true # 409 на повторном прогоне — ок
RID=$(curl -fsS -b "$JAR_ADMIN" "$BASE/api/role-requests" | sed -n 's/.*"id":\([0-9]*\),"login":"e2euser","motivation":"e2e","status":"pending".*/\1/p' | head -1)
[ -n "$RID" ] && curl -fsS -X POST -b "$JAR_ADMIN" "$BASE/api/role-requests/$RID/approve" >/dev/null
curl -fsS -c "$JAR_USER" -b "$JAR_USER" -H 'Content-Type: application/json' -d '{"login":"e2euser","password":"E2e-user-pass-1!"}' "$BASE/api/login" >/dev/null # сессия с новой ролью
curl -fsS -X POST -b "$JAR_USER" "$BASE/api/me/runtime/stop" | grep -q sleeping
for _ in $(seq 1 60); do kubectl -n "$NS" get pod "$POD" >/dev/null 2>&1 || break; sleep 2; done
kubectl -n "$NS" get pod "$POD" >/dev/null 2>&1 && { echo "✗ под не удалён после stop"; exit 1; }
kubectl -n "$NS" get pvc -l app.kubernetes.io/name=agctl-dsh-chat | grep -q data- && echo "  ✓ compute удалён, PVC сохранён"

echo "▶ админ видит рантаймы"
curl -fsS -b "$JAR_ADMIN" "$BASE/api/admin/containers" | head -c 300; echo
echo "✓ k8s e2e smoke OK"
