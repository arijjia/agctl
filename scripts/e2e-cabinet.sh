#!/bin/bash
# e2e-cabinet: автоматическая проверка панели кабинета (без ручного клика).
# Эпик night-dsh-plugins; карточки cabinet-plugin-in-image + cabinet-panel-e2e.
#
#   scripts/e2e-cabinet.sh [--skip-docker]
#
# Секция A (docker): собрать/взять образ, поднять agctl serve на тестовом
#   home, создать воркспейс, дождаться running, проверить через шлюз
#   /w/<id>/: boot graph содержит @arijjia/agctl-cabinet.
# Секция B (без docker): изолированный dsh web (night-profile) содержит
#   код панели в combo-бандле; agctl REST отвечает с CORS-заголовками.
# После себя: глушит serve/fake, воркспейс удаляет.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

SKIP_DOCKER="${1:-}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_HOME:-/tmp/agctl-cabinet-e2e-$RUN_ID}"
ADDR="127.0.0.1:18931"
B="http://$ADDR"
IMG="${IMG:-agctl-dsh:0.2.0-night1}"
BIN="${BIN:-/tmp/agctl-cabinet-e2e-bin}"
NIGHT_LOG="/tmp/agctl-cabinet-e2e-night.log"

PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl || { echo "go build не удался" >&2; exit 2; }

AGCTL_PID=""; FAKE_PID=""
cleanup() {
  [ -n "$AGCTL_PID" ] && kill "$AGCTL_PID" >/dev/null 2>&1 || true
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  [ -f /tmp/agctl-night-dsh-home/web-3555.pid ] && kill "$(cat /tmp/agctl-night-dsh-home/web-3555.pid)" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo "== e2e-cabinet $RUN_ID (img=$IMG) =="

# ── Секция A: воркспейс из образа, панель через шлюз ────────────────────────
if [ "$SKIP_DOCKER" != "--skip-docker" ]; then
  python3 scripts/fake-llm.py --port 18769 >"$TEST_HOME/fake.log" 2>&1 &
  FAKE_PID=$!
  nohup "$BIN" serve --home "$TEST_HOME" --addr "$ADDR" --image "$IMG" \
    --cors-origin "http://127.0.0.1:3555" > "$TEST_HOME/serve.log" 2>&1 &
  AGCTL_PID=$!
  for i in $(seq 1 15); do sleep 1; curl -s -o /dev/null "$B/api/me" && break; done

  curl -s -c "$TEST_HOME/jar" -H 'Content-Type: application/json' \
    -d '{"login":"cabe2e","password":"cabinet-e2e-pass-1"}' "$B/api/register" \
    -o /dev/null
  code=$(curl -s -o /dev/null -w '%{http_code}' -b "$TEST_HOME/jar" "$B/api/me")
  [ "$code" = "200" ] && pass "agctl serve: регистрация+сессия" || fail "agctl serve /api/me = $code"

  curl -s -b "$TEST_HOME/jar" -H 'Content-Type: application/json' \
    -d '{"name":"cab-e2e","kit":"base","components":["core-workspace"],"baseURL":"http://host.docker.internal:18769/v1","models":["stub-model"],"apiKey":"sk-e2e-fake-not-a-secret"}' \
    "$B/api/workspaces" -o /dev/null
  WS=""
  for i in $(seq 1 40); do
    sleep 2
    WS=$(curl -s -b "$TEST_HOME/jar" "$B/api/workspaces" | python3 -c "
import sys, json
d = json.load(sys.stdin)
w = d['workspaces'][0] if d['workspaces'] else {}
print((w['id'] + ' ' + w['status']) if w else '')")
    case "$WS" in *running*) break;; esac
  done
  case "$WS" in
    *running*)
      pass "воркспейс создан и running ($WS)"
      WSID="${WS%% *}"
      html=$(curl -s -b "$TEST_HOME/jar" -L "$B/w/$WSID/")
      echo "$html" | grep -q 'agctl-cabinet' \
        && pass "шлюз /w/<id>: boot graph содержит @arijjia/agctl-cabinet" \
        || fail "шлюз /w/<id>: панели нет в boot graph"
      ;;
    *)
      fail "воркспейс не поднялся: '$WS' (лог: $TEST_HOME/serve.log)"
      ;;
  esac
  [ -n "$WSID" ] && curl -s -b "$TEST_HOME/jar" -X DELETE "$B/api/workspaces/$WSID" -o /dev/null
  kill "$AGCTL_PID" 2>/dev/null; AGCTL_PID=""
  kill "$FAKE_PID" 2>/dev/null; FAKE_PID=""
else
  echo "== секция A пропущена (--skip-docker) =="
fi

# ── Секция B: изолированный dsh web с панелью (night-profile) ──────────────
scripts/night-profile.sh >"$NIGHT_LOG" 2>&1 || fail "night-profile не поднялся"
if [ -f /tmp/agctl-night-dsh-home/web-3555.log ]; then
  T=$(grep -o 'token=[A-Za-z0-9_-]*' /tmp/agctl-night-dsh-home/web-3555.log | head -1 | cut -d= -f2)
  curl -s -c /tmp/agctl-cabinet-e2e-jar "http://127.0.0.1:3555/?token=$T" -o /dev/null
  curl -s -b /tmp/agctl-cabinet-e2e-jar "http://127.0.0.1:3555/" -o /tmp/agctl-cabinet-e2e-page.html
  grep -q 'agctl-cabinet' /tmp/agctl-cabinet-e2e-page.html \
    && pass "night-profile: панель в boot graph прямого dsh web" \
    || fail "night-profile: панели нет в boot graph"
else
  fail "night-profile: нет лога веба"
fi

echo "== итог: PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" -eq 0 ]
