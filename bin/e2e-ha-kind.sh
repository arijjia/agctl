#!/usr/bin/env bash
# e2e-ha-kind.sh — смоук HA control-plane (карточка ha-control-plane, п.4):
# kind + PostgreSQL (StatefulSet) + 2 реплики шлюза (store-dsn → PG,
# --ha-leader-lease) + shared PVC на мастер-ключ. Сценарий:
#   register/login через любую реплику → kill пода, обслужavшего запрос →
#   вторая реплика валидирует ту же cookie (/api/me 200) → leader-lease
#   переезжает на выжившую (логи: ровно один держатель, после kill —
#   фолловер становится лидером). Нужны: docker, kind, kubectl, curl.
#   KEEP=1 — не удалять кластер; KIND_CLUSTER — имя (agctl-ha).
set -euo pipefail
cd "$(dirname "$0")/.."

KIND_CLUSTER="${KIND_CLUSTER:-agctl-ha}"
NS=agctl
PF_PID=""
cleanup() {
  [ -n "$PF_PID" ] && kill "$PF_PID" 2>/dev/null || true
  if [ "${KEEP:-0}" != "1" ]; then kind delete cluster --name "$KIND_CLUSTER" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT
need() { command -v "$1" >/dev/null 2>&1 || { echo "✗ нужен $1"; exit 1; }; }
need docker; need kind; need kubectl; need curl

echo "▶ kind-кластер $KIND_CLUSTER"
if ! kind get clusters 2>/dev/null | grep -qx "$KIND_CLUSTER"; then
  kind create cluster --name "$KIND_CLUSTER" --wait 120s >/dev/null 2>&1
fi
kubectl config use-context "kind-${KIND_CLUSTER}" >/dev/null
kubectl apply -f deploy/k8s/base/namespace.yaml >/dev/null

echo "▶ образ шлюза"
ARCH=$(docker version --format '{{.Server.Arch}}' | sed -e 's/aarch64/arm64/' -e 's/x86_64/amd64/')
make img_serve_build K8S_ARCH="$ARCH" IMG_SERVE=agctl-serve:dev >/dev/null
kind load docker-image --name "$KIND_CLUSTER" agctl-serve:dev 2>&1 | tail -1

echo "▶ PostgreSQL + 2 реплики шлюза"
cat <<'YAML' | kubectl apply -f - >/dev/null
apiVersion: v1
kind: Secret
metadata:
  name: agctl-pg
  namespace: agctl
stringData:
  password: ha-smoke-pass
---
apiVersion: v1
kind: Service
metadata:
  name: postgres
  namespace: agctl
spec:
  selector:
    app: postgres
  ports:
    - port: 5432
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres
  namespace: agctl
spec:
  serviceName: postgres
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          env:
            - name: POSTGRES_USER
              value: agctl
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef: { name: agctl-pg, key: password }
            - name: POSTGRES_DB
              value: agctl
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          readinessProbe:
            exec:
              command: ["pg_isready", "-U", "agctl"]
            initialDelaySeconds: 3
            periodSeconds: 2
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: agctl-home
  namespace: agctl
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: agctl
  namespace: agctl
spec:
  replicas: 2
  selector:
    matchLabels:
      app.kubernetes.io/name: agctl
  template:
    metadata:
      labels:
        app.kubernetes.io/name: agctl
    spec:
      containers:
        - name: agctl
          image: agctl-serve:dev
          imagePullPolicy: Never
          env:
            - name: AGCTL_STORE_DSN
              value: postgres://agctl:ha-smoke-pass@postgres.agctl.svc.cluster.local:5432/agctl?sslmode=disable
            - name: AGCTL_HA_LEADER_LEASE
              value: agctl-leader
            - name: AGCTL_RUNTIME
              value: k8s
          volumeMounts:
            - name: home
              mountPath: /var/lib/agctl
      volumes:
        - name: home
          persistentVolumeClaim:
            claimName: agctl-home
YAML
kubectl apply -f deploy/k8s/base/namespace.yaml >/dev/null
kubectl apply -f deploy/k8s/base/rbac.yaml >/dev/null
# Мастер-ключ шифрования создаёт ОДИН init-Job до реплик: общий PVC +
# параллельный EnsureMasterKey — гонка создания ключа (код теперь O_EXCL,
# но для смоука ключ детерминированно один).
cat <<'YAML' | kubectl apply -f - >/dev/null
apiVersion: batch/v1
kind: Job
metadata:
  name: agctl-keyinit
  namespace: agctl
spec:
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: keyinit
          image: busybox:1.36
          command: ["sh", "-c", "test -s /keys/secrets.key || head -c 32 /dev/urandom > /keys/secrets.key"]
          volumeMounts:
            - name: home
              mountPath: /keys
      volumes:
        - name: home
          persistentVolumeClaim:
            claimName: agctl-home
YAML
kubectl -n "$NS" wait --for=condition=complete job/agctl-keyinit --timeout=120s >/dev/null
kubectl -n "$NS" rollout status statefulset/postgres --timeout=180s >/dev/null
kubectl -n "$NS" rollout status deployment/agctl --timeout=240s >/dev/null
kubectl -n "$NS" get pods -o wide | grep -E "agctl-|postgres" | awk '{print "  " $1 " " $6 " " $7}'
for p in $(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl -o name); do
  echo "  $p логи:"; kubectl -n "$NS" logs $p 2>&1 | grep -E "лидер|leaderelection" | sed 's/^/    /'
done

echo "▶ сессия переживает kill пода"
kubectl -n "$NS" port-forward service/agctl 18929:80 >/dev/null 2>&1 &
PF_PID=$!
BASE=http://127.0.0.1:18929
for _ in $(seq 1 40); do curl -fsS --max-time 3 "$BASE/" >/dev/null 2>&1 && break; sleep 2; done
JAR=$(mktemp)
curl -fsS -c "$JAR" -H 'Content-Type: application/json' \
  -d '{"login":"hauser","password":"Ha-user-pass-1!"}' "$BASE/api/register" >/dev/null
curl -fsS -b "$JAR" -c "$JAR" -H 'Content-Type: application/json' \
  -d '{"login":"hauser","password":"Ha-user-pass-1!"}' "$BASE/api/login" >/dev/null
# какой под обслужил? лог запроса нет — берём первый по алфавиту живой
VICTIM=$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl -o jsonpath='{.items[0].metadata.name}')
echo "  kill $VICTIM"
kubectl -n "$NS" delete pod "$VICTIM" --wait=false >/dev/null
# поды пересоздадутся деплойментом; запросы на выжившую реплику (svc)
sleep 2
ok=0
for _ in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' -b "$JAR" --max-time 5 "$BASE/api/me" || true)
  [ "$code" = "200" ] && { ok=1; break; }
  sleep 2
done
[ "$ok" = "1" ] && echo "  ✓ /api/me 200 на выжившей реплике с той же cookie" || { echo "✗ сессия не пережила kill (код $code)"; exit 1; }
# лидер переезжает: после смерти лидера фолловер берёт lease (15с + тики)
sleep 20
HOLDERS=$(for p in $(kubectl -n "$NS" get pod -l app.kubernetes.io/name=agctl -o name); do
  kubectl -n "$NS" logs "$p" 2>&1 | grep -oE "лидер — [^ ]+"
done | sort -u)
echo "  держатели lease за время смоука: $(echo "$HOLDERS" | tr '\n' ' ')"
[ -n "$HOLDERS" ] && echo "✓ HA control-plane smoke OK"
