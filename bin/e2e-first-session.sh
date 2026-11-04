#!/bin/bash
# e2e-first-session: браузерная регрессия дефекта D6 (кейс Б7 протокола
# РТиППО) на изолированном стенде: «чистый браузер → первый запуск →
# композер активен, сессия открыта, фатальных ошибок консоли нет».
#
#   bin/e2e-first-session.sh
#   IMG=agctl-dsh:0.2.2 AGCTL_BIN=dist/agctl bin/e2e-first-session.sh
#
# Переменные:
#   IMG        образ чат-контейнера (по умолчанию agctl-dsh:0.2.2)
#   AGCTL_BIN  бинарь (по умолчанию собирается из текущего дерева)
#   E2E_ADDR   адрес стенда (по умолчанию 127.0.0.1:18937)
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_UI_HOME:-/tmp/agctl-first-session-$RUN_ID}"
ADDR="${E2E_ADDR:-127.0.0.1:18937}"
BASE="http://$ADDR"
IMG="${IMG:-agctl-dsh:0.2.2}"
BIN="${AGCTL_BIN:-$TEST_HOME/agctl}"
LOGIN="demo-user"
PASSWORD="DemoUser-2026!"

if [ ! -d tests/e2e-ui/node_modules/playwright ]; then
  echo "Playwright dependency missing. Run: cd tests/e2e-ui && npm ci && npx playwright install chromium" >&2
  exit 2
fi
if ! docker image inspect "$IMG" >/dev/null 2>&1; then
  echo "Docker image $IMG missing. Build it (make img_build) or pass IMG=..." >&2
  exit 2
fi
if ! nc -z 127.0.0.1 "${ADDR##*:}" 2>/dev/null; then :; else
  echo "порт ${ADDR##*:} занят — выбери другой E2E_ADDR" >&2; exit 2
fi

mkdir -p "$TEST_HOME"
umask 077
AGCTL_PID=""; SERVE_PID=""
cleanup() {
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" >/dev/null 2>&1
  [ -n "$AGCTL_PID" ] && kill "$AGCTL_PID" >/dev/null 2>&1
  wait "$AGCTL_PID" 2>/dev/null
  # Guard: только контейнеры, чьи маунты принадлежат этому уникальному дому.
  for cid in $(docker ps -aq --filter label=agctl.workspace 2>/dev/null); do
    mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}};{{end}}' "$cid" 2>/dev/null || true)
    case "$mounts" in
      "$TEST_HOME/"*) docker rm -f "$cid" >/dev/null 2>&1 || true;;
    esac
  done
}
trap cleanup EXIT INT TERM

if [ ! -x "$BIN" ]; then
  echo "Сборка бинаря"
  go build -o "$BIN" ./cmd/agctl || exit 2
fi

"$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" \
  --mem-gb 2 --cpus 2 >"$TEST_HOME/serve.log" 2>&1 &
SERVE_PID=$!

READY=""
for i in $(seq 1 30); do
  sleep 1
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/me" 2>/dev/null || true)
  [ "$code" = "401" ] && READY=1 && break
done
[ -n "$READY" ] || { echo "agctl did not start; log: $TEST_HOME/serve.log" >&2; tail -25 "$TEST_HOME/serve.log" >&2; exit 1; }

# Основная учётка (первый пользователь = admin), затем демо-юзеры.
curl -s -H 'Content-Type: application/json' \
  -d '{"login":"owner","password":"Owner-Pass-1"}' "$BASE/api/register" -o /dev/null
"$BIN" seed-demo-users --home "$TEST_HOME/data" >/dev/null 2>&1 || {
  echo "seed-demo-users failed" >&2; exit 1; }

E2E_SHOT_PREFIX="$TEST_HOME/first-session" \
  node tests/e2e-ui/first-session-ui.mjs "$BASE" "$LOGIN" "$PASSWORD"
STATUS=$?

[ $STATUS -eq 0 ] && echo "E2E_FIRST_SESSION_HOME=$TEST_HOME"
exit $STATUS
