#!/bin/bash
# Browser-level product-flow test: domain -> auth -> default DSH chat -> Cabinet
# plugin -> request -> admin approval -> dev workspace.
#
# Prerequisites: npm ci && npx playwright install chromium in tests/e2e-ui/;
# Docker image (IMG=...) and dsh CLI available. Uses isolated /tmp home/ports.
set -u
cd "$(dirname "$0")/.."

RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_UI_HOME:-/tmp/agctl-cabinet-playwright-$RUN_ID}"
ADDR="127.0.0.1:${E2E_UI_PORT:-18936}"
BASE="http://$ADDR"
FAKE_PORT="${E2E_UI_FAKE_PORT:-18772}"
IMG="${IMG:-agctl-dsh:0.2.0-ssh-night5}"
BIN="$TEST_HOME/agctl"
LOGIN="playwright-admin"
PASSWORD="playwright-admin-pass-1"

if [ ! -d tests/e2e-ui/node_modules/playwright ]; then
  echo "Playwright dependency missing. Run: cd tests/e2e-ui && npm ci && npx playwright install chromium" >&2
  exit 2
fi
if ! docker image inspect "$IMG" >/dev/null 2>&1; then
  echo "Docker image $IMG missing. Build it or pass IMG=..." >&2
  exit 2
fi

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl || exit 2
AGCTL_PID=""; FAKE_PID=""; WSID=""
cleanup() {
  if [ -n "$AGCTL_PID" ]; then
    if [ ! -f "$TEST_HOME/jar" ]; then
      curl -s -c "$TEST_HOME/jar" -H 'Content-Type: application/json' \
        -d "{\"login\":\"$LOGIN\",\"password\":\"$PASSWORD\"}" "$BASE/api/login" -o /dev/null 2>/dev/null || true
    fi
    if [ -f "$TEST_HOME/jar" ]; then
      LIST=$(curl -s -b "$TEST_HOME/jar" "$BASE/api/workspaces" 2>/dev/null || true)
      WSID=$(printf '%s' "$LIST" | python3 -c "import json,sys; d=json.load(sys.stdin); print(next((w['id'] for w in d.get('workspaces',[]) if w.get('name')=='playwright-ui-workspace'),''))" 2>/dev/null || true)
      if [ -n "$WSID" ]; then
        curl -s -b "$TEST_HOME/jar" -X DELETE "$BASE/api/workspaces/$WSID" -o /dev/null 2>/dev/null || true
      fi
    fi
  fi
  if [ -n "$AGCTL_PID" ]; then
    kill "$AGCTL_PID" >/dev/null 2>&1 || true
    wait "$AGCTL_PID" 2>/dev/null || true
  fi
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  # Guard: only remove containers whose bind mounts belong to this unique test home.
  for cid in $(docker ps -aq --filter label=agctl.workspace 2>/dev/null); do
    mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}};{{end}}' "$cid" 2>/dev/null || true)
    case "$mounts" in
      "$TEST_HOME/"*) docker rm -f "$cid" >/dev/null 2>&1 || true;;
    esac
  done
}
trap cleanup EXIT INT TERM

python3 scripts/fake-llm.py --port "$FAKE_PORT" --bind 0.0.0.0 >"$TEST_HOME/fake.log" 2>&1 &
FAKE_PID=$!
"$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" \
  --require-approval --chat-ttl-min 15 >"$TEST_HOME/serve.log" 2>&1 &
AGCTL_PID=$!

READY=""
for i in $(seq 1 30); do
  sleep 1
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/me" 2>/dev/null || true)
  [ "$code" = "401" ] && READY=1 && break
done
if [ -z "$READY" ]; then
  echo "agctl did not start; log: $TEST_HOME/serve.log" >&2
  tail -25 "$TEST_HOME/serve.log" >&2
  exit 1
fi

export PLATFORM_URL="$BASE"
export E2E_LOGIN="$LOGIN"
export E2E_PASSWORD="$PASSWORD"
export E2E_LLM_BASE_URL="http://host.docker.internal:$FAKE_PORT/v1"
export E2E_SCREENSHOT="$TEST_HOME/result.png"
if ! node tests/e2e-ui/cabinet-ui.mjs; then
  echo "Playwright flow failed; screenshot/logs: $TEST_HOME" >&2
  exit 1
fi

# Save agctl cookie for cleanup, then verify that approval created a real workspace.
curl -s -c "$TEST_HOME/jar" -H 'Content-Type: application/json' \
  -d "{\"login\":\"$LOGIN\",\"password\":\"$PASSWORD\"}" "$BASE/api/login" -o /dev/null
LIST=$(curl -s -b "$TEST_HOME/jar" "$BASE/api/workspaces")
WSID=$(printf '%s' "$LIST" | python3 -c "import json,sys; d=json.load(sys.stdin); print(next((w['id'] for w in d.get('workspaces',[]) if w.get('name')=='playwright-ui-workspace'),''))")
[ -n "$WSID" ] || { echo "Playwright passed but approved workspace missing from API" >&2; exit 1; }

echo "E2E_PLAYWRIGHT_HOME=$TEST_HOME"
