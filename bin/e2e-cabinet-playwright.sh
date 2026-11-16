#!/bin/bash
# Browser-level v3 cabinet UX test: domain -> auth -> default DSH chat
# (profile branding, hidden dev controls for the user role) -> cabinet
# profile chip -> popup -> Профиль/SSH-доступ overlays after the developer
# role upgrade. Workspace-request flow is demolished in v3 (410 handlers).
#
# Prerequisites: npm ci && npx playwright install chromium in tests/e2e-ui/;
# Docker image (IMG=..., dsh 0.2.2+). Uses isolated /tmp home/ports.
set -u
cd "$(dirname "$0")/.."

RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_UI_HOME:-/tmp/agctl-cabinet-playwright-$RUN_ID}"
ADDR="127.0.0.1:${E2E_UI_PORT:-18936}"
BASE="http://$ADDR"
FAKE_PORT="${E2E_UI_FAKE_PORT:-18772}"
LLM_BASE_URL="http://host.docker.internal:$FAKE_PORT/v1"
IMG="${IMG:-agctl-dsh:0.2.2}"
BIN="$TEST_HOME/agctl"
ADMIN_LOGIN="cabinet-admin"
ADMIN_PASSWORD="cabinet-admin-pass-1"
LOGIN="cabinet-user"
PASSWORD="cabinet-user-pass-1"

if [ ! -d tests/e2e-ui/node_modules/playwright ]; then
  echo "Playwright dependency missing. Run: cd tests/e2e-ui && npm ci && npx playwright install chromium" >&2
  exit 2
fi
command -v ssh-keygen >/dev/null || { echo "ssh-keygen не найдена" >&2; exit 2; }
if ! docker image inspect "$IMG" >/dev/null 2>&1; then
  echo "Docker image $IMG missing. Build it or pass IMG=..." >&2
  exit 2
fi

mkdir -p "$TEST_HOME"
go build -o "$BIN" ./cmd/agctl || exit 2
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519"
PUBKEY="$(cat "$TEST_HOME/id_ed25519.pub")"
AGCTL_PID=""; FAKE_PID=""
cleanup() {
  if [ -n "$AGCTL_PID" ]; then
    kill "$AGCTL_PID" >/dev/null 2>&1 || true
    wait "$AGCTL_PID" 2>/dev/null || true
  fi
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  # Guard: only remove containers whose bind mounts belong to this unique test home.
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

python3 bin/fake-llm.py --port "$FAKE_PORT" --bind 0.0.0.0 >"$TEST_HOME/fake.log" 2>&1 &
FAKE_PID=$!
AGCTL_LLM_API_KEY=e2e-fake-key-not-a-secret "$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" \
  --llm-base-url "$LLM_BASE_URL" \
  --llm-model stub-model --llm-models stub-model >"$TEST_HOME/serve.log" 2>&1 &
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

# First account on a fresh server becomes admin (no personal chat, /admin
# panel): create it via the API so the browser flow registers a plain user.
curl -s -c "$TEST_HOME/admin.jar" -H 'Content-Type: application/json' \
  -d "{\"login\":\"$ADMIN_LOGIN\",\"password\":\"$ADMIN_PASSWORD\"}" "$BASE/api/register" -o /dev/null

export PLATFORM_URL="$BASE"
export E2E_LOGIN="$LOGIN"
export E2E_PASSWORD="$PASSWORD"
export E2E_ADMIN_LOGIN="$ADMIN_LOGIN"
export E2E_ADMIN_PASSWORD="$ADMIN_PASSWORD"
export E2E_LLM_BASE_URL="$LLM_BASE_URL"
export E2E_SSH_PUBKEY="$PUBKEY"
export E2E_SCREENSHOT="$TEST_HOME/result.png"
if ! node tests/e2e-ui/cabinet-ui.mjs; then
  echo "Playwright flow failed; screenshot/logs: $TEST_HOME" >&2
  exit 1
fi

echo "E2E_PLAYWRIGHT_HOME=$TEST_HOME"
