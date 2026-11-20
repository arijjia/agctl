#!/bin/bash
# Browser-level v3 product flow: user register -> developer role request ->
# admin approve -> SSH pubkey -> project folder -> session in project cwd,
# then a REAL SSH login into the personal container (key-only, loopback port).
#
#   IMG=agctl-dsh-server:0.2.2 bin/e2e-developer-flow.sh
#
# Prerequisites: npm ci && npx playwright install chromium in tests/e2e-ui/;
# built server image agctl-dsh-server (make img_build_server IMG_SERVER=...); ssh-keygen, ssh, python3.
# Uses an isolated TEST_HOME, its own ports and an ephemeral SSH keypair;
# never prints private-key material.
set -u
cd "$(dirname "$0")/.."

RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_DEVFLOW_HOME:-/tmp/agctl-developer-flow-$RUN_ID}"
ADDR="127.0.0.1:${E2E_DEVFLOW_PORT:-18941}"
BASE="http://$ADDR"
FAKE_PORT="${E2E_DEVFLOW_FAKE_PORT:-18776}"
LLM_BASE_URL="http://host.docker.internal:$FAKE_PORT/v1"
IMG="${IMG:-agctl-dsh-server:0.2.2}"
BIN="$TEST_HOME/agctl"
ADMIN_LOGIN="devflow-admin"
ADMIN_PASSWORD="devflow-admin-pass-1"
LOGIN="devflow-developer"
PASSWORD="devflow-developer-pass-1"
PROJECT="devflow-project"

PASS=0; FAIL=0; AGCTL_PID=""; FAKE_PID=""
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }
cleanup() {
  [ -n "$AGCTL_PID" ] && kill "$AGCTL_PID" >/dev/null 2>&1 && wait "$AGCTL_PID" 2>/dev/null
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1
  # Guard: only remove containers whose bind mounts belong to this test home.
  for cid in $(docker ps -aq --filter label=agctl.chat 2>/dev/null; docker ps -aq --filter label=agctl.workspace 2>/dev/null); do
    mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}};{{end}}' "$cid" 2>/dev/null || true)
    case "$mounts" in
      "$TEST_HOME/"*) docker rm -f "$cid" >/dev/null 2>&1 || true;;
    esac
  done
  # Ephemeral test key is disposable; logs/DB stay for diagnosis.
  rm -f "$TEST_HOME/id_ed25519" "$TEST_HOME/id_ed25519.pub"
}
trap cleanup EXIT INT TERM

if [ ! -d tests/e2e-ui/node_modules/playwright ]; then
  echo "Playwright dependency missing. Run: cd tests/e2e-ui && npm ci && npx playwright install chromium" >&2
  exit 2
fi
command -v ssh-keygen >/dev/null || { echo "ssh-keygen не найден" >&2; exit 2; }
command -v ssh >/dev/null || { echo "ssh-клиент не найден" >&2; exit 2; }
docker image inspect "$IMG" >/dev/null 2>&1 || { echo "image $IMG не найдена; сначала make img_build_server IMG_SERVER=$IMG" >&2; exit 2; }

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl-server || exit 2
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519"
PUBKEY="$(cat "$TEST_HOME/id_ed25519.pub")"

python3 bin/fake-llm.py --port "$FAKE_PORT" --bind 0.0.0.0 >"$TEST_HOME/fake.log" 2>&1 &
FAKE_PID=$!
AGCTL_LLM_API_KEY=e2e-fake-key-not-a-secret "$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" \
  --chat-ttl-min 30 --llm-base-url "$LLM_BASE_URL" \
  --llm-model stub-model --llm-models stub-model >"$TEST_HOME/serve.log" 2>&1 &
AGCTL_PID=$!

READY=""
for i in $(seq 1 30); do
  sleep 1
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/me" 2>/dev/null || true)
  [ "$code" = "401" ] && READY=1 && break
done
if [ -z "$READY" ]; then
  echo "agctl не стартовал; лог: $TEST_HOME/serve.log" >&2
  tail -25 "$TEST_HOME/serve.log" >&2
  exit 1
fi

# First account on a fresh server becomes admin: create it via the API before
# the browser flow registers the developer (second account => role user).
curl -s -c "$TEST_HOME/admin.jar" -H 'Content-Type: application/json' \
  -d "{\"login\":\"$ADMIN_LOGIN\",\"password\":\"$ADMIN_PASSWORD\"}" "$BASE/api/register" -o /dev/null

export PLATFORM_URL="$BASE"
export E2E_LOGIN="$LOGIN" E2E_PASSWORD="$PASSWORD"
export E2E_ADMIN_LOGIN="$ADMIN_LOGIN" E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD"
export E2E_SSH_PUBKEY="$PUBKEY"
export E2E_PROJECT="$PROJECT"
export E2E_LLM_BASE_URL="$LLM_BASE_URL"
export E2E_SCREENSHOT="$TEST_HOME/result.png"

if ! node tests/e2e-ui/developer-flow-ui.mjs; then
  echo "Playwright flow failed; скриншот/логи: $TEST_HOME" >&2
  exit 1
fi
pass "browser flow: register -> role request -> approve -> pubkey -> project -> session"

# ── real SSH channel: key-only login into the personal container ─────────────
curl -s -c "$TEST_HOME/dev.jar" -H 'Content-Type: application/json' \
  -d "{\"login\":\"$LOGIN\",\"password\":\"$PASSWORD\"}" "$BASE/api/login" -o /dev/null
PORT=""
for i in $(seq 1 45); do
  PORT=$(curl -s -b "$TEST_HOME/dev.jar" "$BASE/api/me/ssh" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("sshPort",0))' 2>/dev/null || true)
  [ "${PORT:-0}" -gt 0 ] 2>/dev/null && break
  sleep 2
done
if [ -z "$PORT" ] || [ "$PORT" = "0" ]; then
  fail "sshPort не появился (контейнер не поднял sshd)"; echo "E2E_DEVFLOW_HOME=$TEST_HOME"; exit 1
fi
pass "live sshPort=$PORT"

SSHOPTS="-i $TEST_HOME/id_ed25519 -p $PORT -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=10 -o BatchMode=yes"
WHO=$(ssh $SSHOPTS root@127.0.0.1 'whoami' 2>>"$TEST_HOME/ssh.log")
[ "$WHO" = "root" ] && pass "SSH key-only вход (whoami=$WHO)" || fail "SSH whoami='$WHO' (лог: $TEST_HOME/ssh.log)"

WORKSPACE=$(ssh $SSHOPTS root@127.0.0.1 "ls -d /workspace/$PROJECT 2>/dev/null" 2>>"$TEST_HOME/ssh.log")
[ "$WORKSPACE" = "/workspace/$PROJECT" ] && pass "проект виден в контейнере как /workspace/$PROJECT" \
  || fail "каталог /workspace/$PROJECT не найден в контейнере (лог: $TEST_HOME/ssh.log)"

GITDIR=$(ssh $SSHOPTS root@127.0.0.1 "test -d /workspace/$PROJECT/.git && echo git-ok" 2>>"$TEST_HOME/ssh.log")
[ "$GITDIR" = "git-ok" ] && pass "в проекте есть локальный git-репозиторий (autosave)" || fail "нет .git в /workspace/$PROJECT"

if [ "$FAIL" -gt 0 ]; then
  echo "E2E_DEVFLOW_HOME=$TEST_HOME"
  exit 1
fi
echo "PASS=$PASS FAIL=$FAIL  E2E_DEVFLOW_HOME=$TEST_HOME"
