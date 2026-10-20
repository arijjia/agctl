#!/bin/bash
# e2e-local-bridge — real local DSH behind the experimental outbound bridge.
#
# Proves the full carrier chain with the pinned host DSH binary (no Docker, no
# fork): server pairing API → `agctl local connect --launch-dsh` → boot-token
# scrape → 401 mint through the tunnel → real DSH web UI served on the agctl
# origin. Isolated /tmp home, loopback ports only; never touches :8929/:8930.
#
# Not covered (still open on the bridge card): reconnect/heartbeat, Cabinet UX,
# security review. Passing this test does NOT make local mode production-ready.
set -u
cd "$(dirname "$0")/.."

RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${LOCAL_BRIDGE_HOME:-/tmp/agctl-local-bridge-$RUN_ID}"
ADDR="127.0.0.1:${LOCAL_BRIDGE_PORT:-18973}"
BASE="http://$ADDR"
DSH_PORT="${LOCAL_BRIDGE_DSH_PORT:-19931}"
BIN="$TEST_HOME/agctl"
LOGIN="bridge-admin"
PASSWORD="bridge-admin-pass-1"

command -v dsh >/dev/null 2>&1 || { echo "local dsh binary not found; this e2e requires the pinned host dsh" >&2; exit 2; }

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl || exit 2

SERVE_PID=""; CONNECT_PID=""
cleanup() {
  [ -n "$CONNECT_PID" ] && kill "$CONNECT_PID" >/dev/null 2>&1
  sleep 1 # connector context also stops its launched dsh web child
  pkill -f "dsh web --port $DSH_PORT" >/dev/null 2>&1
  [ -n "$SERVE_PID" ] && { kill "$SERVE_PID" >/dev/null 2>&1; wait "$SERVE_PID" 2>/dev/null; }
  sleep 1 # let the killed dsh release its npm cache before removal
  rm -rf "$TEST_HOME" 2>/dev/null || rm -rf "$TEST_HOME"
}
trap cleanup EXIT INT TERM

pass() { echo "  ok: $1"; }
fail() { echo "  FAIL: $1" >&2; exit 1; }

# 1. Server with the experimental pairing API (loopback addr → http allowed).
"$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" \
  --experimental-local-bridge >"$TEST_HOME/serve.log" 2>&1 &
SERVE_PID=$!
for i in $(seq 1 30); do
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$BASE/api/config" 2>/dev/null || true)
  [ "$code" = "200" ] && break
  sleep 1
done
[ "$code" = "200" ] || { tail -20 "$TEST_HOME/serve.log" >&2; fail "agctl did not start"; }
pass "agctl serve: experimental-local-bridge on loopback"

# 2. User session + one-time pairing grant.
curl -s -c "$TEST_HOME/jar" -H 'Content-Type: application/json' \
  -d "{\"login\":\"$LOGIN\",\"password\":\"$PASSWORD\"}" "$BASE/api/register" -o /dev/null
GRANT=$(curl -s -b "$TEST_HOME/jar" -X POST "$BASE/api/local-backend/grant" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["grant"])')
[ -n "$GRANT" ] || fail "pairing grant was not issued"
pass "pairing grant issued (bearer, single-use)"

# 3. Connector launches the pinned local DSH on loopback and scrapes its token.
#    Isolated HOME keeps the owner's real ~/.dsh untouched.
export HOME="$TEST_HOME/host-home"
mkdir -p "$HOME"
"$BIN" local connect --server "$BASE" --grant "$GRANT" \
  --credential-file "$TEST_HOME/connector.json" \
  --launch-dsh --dsh-port "$DSH_PORT" >"$TEST_HOME/connect.log" 2>&1 &
CONNECT_PID=$!

CONNECTED=""
for i in $(seq 1 30); do
  status=$(curl -s -b "$TEST_HOME/jar" "$BASE/api/local-backend/status")
  echo "$status" | grep -q '"connected":true' && CONNECTED=1 && break
  sleep 1
done
[ -n "$CONNECTED" ] || { tail -20 "$TEST_HOME/connect.log" "$TEST_HOME/serve.log" >&2; fail "connector never became connected"; }
pass "local connector paired over outbound WSS+yamux"

# 4. Real DSH UI through the tunnel: first hit 401→mint→303, cookie jar gets
#    dsh-auth, followed request serves the actual DSH web HTML on the agctl
#    origin. A few retries mirror a user refreshing while the boot token is
#    still in flight over the control stream.
html=""
for i in $(seq 1 10); do
  code=$(curl -s -b "$TEST_HOME/jar" -c "$TEST_HOME/jar" -L --max-time 30 \
    -o "$TEST_HOME/ui.html" -w '%{http_code}' "$BASE/api/local-backend/ui/")
  [ "$code" = "200" ] && html=$(cat "$TEST_HOME/ui.html") && break
  sleep 1
done
[ "$code" = "200" ] || fail "UI through tunnel never reached 200 (last=$code)"
echo "$html" | grep -qi 'dsh' && echo "$html" | grep -q '<script' \
  && pass "real pinned DSH web UI served through the outbound tunnel" \
  || fail "UI through tunnel is not the real DSH web page"

# 5. Boot token never reaches server logs (it lives on the connector side).
TOKEN=$(grep -o 'token=[A-Za-z0-9_-]*' "$TEST_HOME/connect.log" | head -1 | cut -d= -f2)
if [ -n "$TOKEN" ] && grep -q "$TOKEN" "$TEST_HOME/serve.log"; then
  fail "boot token leaked into server logs"
fi
pass "boot token stays out of server logs"

# 5b. Durable credential: stop the grant-based connector and reconnect with the
#     saved credential file — no new grant needed.
kill "$CONNECT_PID" >/dev/null 2>&1
sleep 2
"$BIN" local connect --server "$BASE" \
  --credential-file "$TEST_HOME/connector.json" \
  --launch-dsh --dsh-port "$DSH_PORT" >"$TEST_HOME/connect2.log" 2>&1 &
CONNECT_PID=$!
CONNECTED=""
for i in $(seq 1 30); do
  status=$(curl -s -b "$TEST_HOME/jar" "$BASE/api/local-backend/status")
  echo "$status" | grep -q '"connected":true' && CONNECTED=1 && break
  sleep 1
done
[ -n "$CONNECTED" ] || { tail -20 "$TEST_HOME/connect2.log" >&2; fail "credential reconnect failed"; }
pass "durable credential reconnects without a new grant"

# 6. Revoke closes the connector session and deletes the credential record.
curl -s -b "$TEST_HOME/jar" -X DELETE "$BASE/api/local-backend" -o /dev/null
sleep 2
kill -0 "$CONNECT_PID" 2>/dev/null && fail "connector survived revoke"
pass "revoke closes the outbound connector"

echo "E2E_LOCAL_BRIDGE_HOME=$TEST_HOME"
