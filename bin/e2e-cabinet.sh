#!/bin/bash
# e2e-cabinet: автоматическая проверка панели кабинета (без ручного клика).
# Эпик night-dsh-plugins; карточки cabinet-plugin-in-image + cabinet-panel-e2e.
#
#   bin/e2e-cabinet.sh [--skip-docker]
#
# Секция B (без docker): изолированный dsh web (night-profile) содержит
#   код панели в combo-бандле. Секция C (docker): multi-user Tier 0 через
#   /chat/ — personal dsh runtimes, изоляция volumes; v2-воркспейсы и
#   шлюз /w/ снесены в v3 (карточка legacy-workspace-code-cleanup).
# После себя: глушит serve/fake.
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

PASS=0; FAIL=0
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl-server || { echo "go build не удался" >&2; exit 2; }

AGCTL_PID=""; FAKE_PID=""
cleanup() {
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

# ── Секция B: изолированный dsh web с панелью (night-profile) ──────────────
DSH_HOME_TEST="$NIGHT_HOME" PORT="$NIGHT_PORT" bin/night-profile.sh >"$NIGHT_LOG" 2>&1 || fail "night-profile не поднялся"
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
  python3 bin/fake-llm.py --port 18769 >"$TEST_HOME/fake.log" 2>&1 &
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

  code=$(curl -s -o "$TEST_HOME/root-anon.html" -w '%{http_code}' "$B/")
  if [ "$code" = "200" ] && grep -q 'agctl' "$TEST_HOME/root-anon.html"; then
    pass "корень без сессии — страница входа SPA (HTTP 200)"
  else
    fail "корень без сессии не отдал страницу входа (HTTP $code)"
  fi
  curl -s -o /dev/null -w '%{redirect_url}' -b "$TEST_HOME/jar" "$B/" > "$TEST_HOME/root-admin.url"
  grep -q '/admin' "$TEST_HOME/root-admin.url" \
    && pass "корень админа → /admin (личный чат админу не выдаётся)" \
    || fail "корень админа не ведёт в /admin: $(cat "$TEST_HOME/root-admin.url")"

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

  # Чат — только не-админам: admin → /admin, пользователь платформы → /chat/.
  curl -s -c "$TEST_HOME/user1.jar" -H 'Content-Type: application/json' \
    -d '{"login":"cabinet-student","password":"student-pass-1"}' \
    "$B/api/register" -o /dev/null
  check_chat "first user" "$TEST_HOME/user1.jar" "$TEST_HOME/chat-user1.html"
  grep -q 'dsh-auth-' "$TEST_HOME/user1.jar" && grep -q '/chat/' "$TEST_HOME/user1.jar" \
    && pass "DSH auth cookie path-scoped to /chat/" \
    || fail "DSH cookie не ограничена путём /chat/"

  curl -s -c "$TEST_HOME/user2.jar" -H 'Content-Type: application/json' \
    -d '{"login":"cabinet-student2","password":"student-pass-2"}' \
    "$B/api/register" -o /dev/null
  check_chat "second user" "$TEST_HOME/user2.jar" "$TEST_HOME/chat-user2.html"

  # Exercise the public DSH Remote API through the same /chat gateway. A first
  # session.create allocates a hidden persistent cwd before returning.
  CREATE_ARGS='{"request":{"workspaceId":"ws-agctl"}}'
  CREATE_BODY=$(python3 - "session/create" "$CREATE_ARGS" <<'PY'
import json, sys, uuid
print(json.dumps({"type":"client-request", "rpcId":str(uuid.uuid4()), "method":sys.argv[1], "payload":{"args":json.loads(sys.argv[2])}}))
PY
)
  CREATE_RESP=$(curl -s -b "$TEST_HOME/user1.jar" -c "$TEST_HOME/user1.jar" -H 'Content-Type: application/json' \
    -d "$CREATE_BODY" "$B/chat/api/session/create")
  SESSION_ID=$(printf '%s' "$CREATE_RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); r=d.get('result',{}); v=r.get('value',{}) if isinstance(r,dict) else {}; print(v.get('sessionId',''))" 2>/dev/null || true)
  if [ -z "$SESSION_ID" ]; then
    fail "session.create via /chat gateway did not return Session id: $CREATE_RESP"
  else
    LIST_ARGS='{"_request":{}}'
    LIST_BODY=$(python3 - "session/list" "$LIST_ARGS" <<'PY'
import json, sys, uuid
print(json.dumps({"type":"client-request", "rpcId":str(uuid.uuid4()), "method":sys.argv[1], "payload":{"args":json.loads(sys.argv[2])}}))
PY
)
    LIST_RESP=$(curl -s -b "$TEST_HOME/user1.jar" -c "$TEST_HOME/user1.jar" -H 'Content-Type: application/json' \
      -d "$LIST_BODY" "$B/chat/api/session/list")
    SESSION_OK=$(printf '%s' "$LIST_RESP" | python3 -c "import json,sys; d=json.load(sys.stdin); items=d.get('result',{}).get('value',{}).get('items',[]); print(int(any(x.get('sessionId')=='$SESSION_ID' for x in items)))" 2>/dev/null || true)
    [ "$SESSION_OK" = "1" ] \
      && pass "DSH Remote API round-trip: session $SESSION_ID видна в session/list" \
      || fail "session/list не вернул созданную сессию: $LIST_RESP"
  fi

  python3 - "$TEST_HOME" <<'PY'
import json, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1]).resolve()
found = {}
ids = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=agctl.workspace"], text=True).splitlines()
for cid in ids:
    data = json.loads(subprocess.check_output(["docker", "inspect", cid], text=True))[0]
    for mount in data.get("Mounts", []):
        source = pathlib.Path(mount.get("Source", "")).resolve()
        for uid in (2, 3):  # u1 — admin, чата нет
            expected = root / "chats" / f"u{uid}" / "dsh-home"
            if source == expected:
                found[uid] = (data.get("Id"), str(source))
if set(found) != {2, 3} or found[2][0] == found[3][0] or found[2][1] == found[3][1]:
    print("FAIL: per-user chat runtimes/volumes not isolated:", found)
    sys.exit(1)
print("  ok: two users → distinct containers/DSH_HOME volumes")
PY
  [ $? -eq 0 ] && pass "per-user chat runtimes are isolated" || fail "per-user chat isolation check failed"
fi

echo "== итог: PASS=$PASS FAIL=$FAIL =="
[ "$FAIL" -eq 0 ]
