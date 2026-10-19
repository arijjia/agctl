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
NIGHT_HOME="$TEST_HOME/dsh-home"
NIGHT_PORT="${E2E_NIGHT_PORT:-3556}"
NIGHT_LOG="$TEST_HOME/night-profile.log"

PASS=0; FAIL=0; WSID=""
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl || { echo "go build не удался" >&2; exit 2; }

AGCTL_PID=""; FAKE_PID=""
cleanup() {
  if [ -n "$WSID" ] && [ -f "$TEST_HOME/jar" ]; then
    curl -s -b "$TEST_HOME/jar" -X DELETE "$B/api/workspaces/$WSID" -o /dev/null 2>/dev/null || true
    for i in $(seq 1 30); do
      [ -z "$(docker ps -aq --filter "label=agctl.workspace=$WSID" 2>/dev/null)" ] && break
      sleep 1
    done
    if [ -n "$(docker ps -aq --filter "label=agctl.workspace=$WSID" 2>/dev/null)" ]; then
      echo "cleanup: fallback remove own container agctl-$WSID" >&2
      docker rm -f "agctl-$WSID" >/dev/null 2>&1 || true
    fi
  fi
  if [ -n "$AGCTL_PID" ]; then
    kill "$AGCTL_PID" >/dev/null 2>&1 || true
    wait "$AGCTL_PID" 2>/dev/null || true
    AGCTL_PID=""
  fi
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  # Also remove only chat containers whose persistent mounts belong to this TEST_HOME.
  if [ "$SKIP_DOCKER" != "--skip-docker" ]; then
    for cid in $(docker ps -aq --filter label=agctl.workspace 2>/dev/null); do
      mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}};{{end}}' "$cid" 2>/dev/null || true)
      case "$mounts" in
        "$TEST_HOME/chats/"*) docker rm -f "$cid" >/dev/null 2>&1 || true;;
      esac
    done
  fi
  if [ -f "$NIGHT_HOME/web-$NIGHT_PORT.pid" ]; then
    kill "$(cat "$NIGHT_HOME/web-$NIGHT_PORT.pid")" >/dev/null 2>&1 || true
  fi
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
      SSHSTATE=$(curl -s -b "$TEST_HOME/jar" "$B/api/workspaces/$WSID" | python3 -c "import json,sys; w=json.load(sys.stdin)['workspace']; print(w.get('sshEnabled'), w.get('sshPort'))")
      [ "$SSHSTATE" = "False 0" ] && pass "без ключа SSH отключён по умолчанию" || fail "SSH должен быть выключен без ключа, got '$SSHSTATE'"
      BOUND=$(docker port "agctl-$WSID" 2222/tcp 2>/dev/null || true)
      [ -z "$BOUND" ] && pass "без ключа контейнерный порт 2222 не опубликован" || fail "SSH-порт открыт без ключа: $BOUND"
      html=$(curl -s -b "$TEST_HOME/jar" -L "$B/w/$WSID/")
      echo "$html" | grep -q 'agctl-cabinet' \
        && pass "шлюз /w/<id>: boot graph содержит @arijjia/agctl-cabinet" \
        || fail "шлюз /w/<id>: панели нет в boot graph"
      ;;
    *)
      fail "воркспейс не поднялся: '$WS' (лог: $TEST_HOME/serve.log)"
      ;;
  esac
else
  echo "== секция A пропущена (--skip-docker) =="
fi

# ── Секция B: изолированный dsh web с панелью (night-profile) ──────────────
DSH_HOME_TEST="$NIGHT_HOME" PORT="$NIGHT_PORT" scripts/night-profile.sh >"$NIGHT_LOG" 2>&1 || fail "night-profile не поднялся"
if [ -f "$NIGHT_HOME/web-$NIGHT_PORT.log" ]; then
  T=$(grep -o 'token=[A-Za-z0-9_-]*' "$NIGHT_HOME/web-$NIGHT_PORT.log" | head -1 | cut -d= -f2)
  curl -s -c "$TEST_HOME/night-jar" "http://127.0.0.1:$NIGHT_PORT/?token=$T" -o /dev/null
  curl -s -b "$TEST_HOME/night-jar" "http://127.0.0.1:$NIGHT_PORT/" -o "$TEST_HOME/night-page.html"
  grep -q 'agctl-cabinet' "$TEST_HOME/night-page.html" \
    && pass "night-profile: панель в boot graph прямого dsh web (:${NIGHT_PORT})" \
    || fail "night-profile: панели нет в boot graph"
else
  fail "night-profile: нет лога веба"
fi

# ── Секция C: multi-user Tier 0 (общий домен → личные dsh runtime) ──────────
# Корень без сессии ведёт на login; после сессии — в /chat/. Два пользователя
# должны получить разные dsh containers + разные постоянные DSH_HOME volumes.
if [ "$SKIP_DOCKER" != "--skip-docker" ]; then
  code=$(curl -s -o /dev/null -w '%{http_code}' -D "$TEST_HOME/root.headers" "$B/")
  grep -qi '^Location: /app/' "$TEST_HOME/root.headers" \
    && pass "корень без сессии → /app/ (SPA-логин)" \
    || fail "корень без сессии не ведёт на /app/ (HTTP $code)"

  check_chat() {
    local label="$1" jar="$2" out="$3" code=""
    for i in $(seq 1 35); do
      code=$(curl -s -b "$jar" -c "$jar" -L -o "$out" -w '%{http_code}' "$B/" || true)
      if [ "$code" = "200" ] && grep -q 'agctl-cabinet' "$out"; then
        pass "$label: login → root → /chat/ → dsh + Кабинет (HTTP 200)"
        return 0
      fi
      sleep 1
    done
    fail "$label: root/chat не прогрелся (HTTP $code); log=$TEST_HOME/serve.log"
    return 1
  }

  check_chat "admin user" "$TEST_HOME/jar" "$TEST_HOME/chat-user1.html"
  grep -q 'dsh-auth-' "$TEST_HOME/jar" && grep -q '/chat/' "$TEST_HOME/jar" \
    && pass "DSH auth cookie path-scoped to /chat/" \
    || fail "DSH cookie не ограничена путём /chat/"

  curl -s -c "$TEST_HOME/user2.jar" -H 'Content-Type: application/json' \
    -d '{"login":"cabinet-student","password":"student-pass-1"}' \
    "$B/api/register" -o /dev/null
  check_chat "second user" "$TEST_HOME/user2.jar" "$TEST_HOME/chat-user2.html"

  python3 - "$TEST_HOME" <<'PY'
import json, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1]).resolve()
found = {}
ids = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=agctl.workspace"], text=True).splitlines()
for cid in ids:
    data = json.loads(subprocess.check_output(["docker", "inspect", cid], text=True))[0]
    for mount in data.get("Mounts", []):
        source = pathlib.Path(mount.get("Source", "")).resolve()
        for uid in (1, 2):
            expected = root / "chats" / f"u{uid}" / "dsh-home"
            if source == expected:
                found[uid] = (data.get("Id"), str(source))
if set(found) != {1, 2} or found[1][0] == found[2][0] or found[1][1] == found[2][1]:
    print("FAIL: per-user chat runtimes/volumes not isolated:", found)
    sys.exit(1)
print("  ok: two users → distinct DSH containers and persistent DSH_HOME volumes")
PY
  [ $? -eq 0 ] && pass "per-user chat runtimes are isolated" || fail "per-user chat isolation check failed"
fi

echo "== итог: PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" -eq 0 ]
