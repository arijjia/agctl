#!/bin/bash
# e2e-ssh: real Docker SSH-channel test (container public-key auth on 127.0.0.1).
# Requires a built agctl-dsh image with openssh-server (make img_build IMG=...).
#
#   IMG=agctl-dsh:0.2.0-ssh-night scripts/e2e-ssh.sh
#
# The test creates an isolated TEST_HOME + ephemeral SSH key, provisions a
# workspace with sshPubKey, connects with the system ssh client, and cleans
# up the workspace/server. It never prints private-key material.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_SSH_HOME:-/tmp/agctl-ssh-e2e-$RUN_ID}"
ADDR="127.0.0.1:${E2E_SSH_ADDR_PORT:-18932}"
B="http://$ADDR"
IMG="${IMG:-agctl-dsh:0.2.0-ssh-night}"
BIN="$TEST_HOME/agctl"
FAKE_PORT="${FAKE_PORT:-18770}"

PASS=0; FAIL=0; AGCTL_PID=""; FAKE_PID=""; WSID=""
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }
cleanup() {
  if [ -n "$WSID" ] && [ -f "$TEST_HOME/jar" ]; then
    curl -s -b "$TEST_HOME/jar" -X DELETE "$B/api/workspaces/$WSID" -o /dev/null 2>/dev/null || true
    # DELETE is async; wait for agctl to remove our labeled container before
    # stopping the server, so repeated tests don't leak Docker resources.
    for i in $(seq 1 30); do
      [ -z "$(docker ps -aq --filter "label=agctl.workspace=$WSID" 2>/dev/null)" ] && break
      sleep 1
    done
    if [ -n "$(docker ps -aq --filter "label=agctl.workspace=$WSID" 2>/dev/null)" ]; then
      echo "cleanup: fallback remove own container agctl-$WSID" >&2
      docker rm -f "agctl-$WSID" >/dev/null 2>&1 || true
    fi
  fi
  [ -n "$AGCTL_PID" ] && kill "$AGCTL_PID" >/dev/null 2>&1 || true
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  # Test private keys are disposable; keep logs/DB, remove only generated keys.
  rm -f "$TEST_HOME/id_ed25519" "$TEST_HOME/id_ed25519.pub" \
    "$TEST_HOME/id_ed25519_rotated" "$TEST_HOME/id_ed25519_rotated.pub"
}
trap cleanup EXIT INT TERM

mkdir -p "$TEST_HOME"
if [ -e "$TEST_HOME/id_ed25519" ] || [ -e "$TEST_HOME/id_ed25519_rotated" ]; then
  echo "тестовые ключи уже существуют в $TEST_HOME; выбери новый E2E_SSH_HOME" >&2
  exit 2
fi
command -v ssh-keygen >/dev/null || { echo "ssh-keygen не найден" >&2; exit 2; }
command -v ssh >/dev/null || { echo "ssh-клиент не найден" >&2; exit 2; }
docker image inspect "$IMG" >/dev/null 2>&1 || { echo "image $IMG не найден; сначала make img_build IMG=$IMG" >&2; exit 2; }

go build -o "$BIN" ./cmd/agctl || exit 2
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519"
python3 scripts/fake-llm.py --port "$FAKE_PORT" >"$TEST_HOME/fake.log" 2>&1 &
FAKE_PID=$!
"$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" >"$TEST_HOME/serve.log" 2>&1 &
AGCTL_PID=$!
for i in $(seq 1 20); do
  sleep 1
  code=$(curl -s -o /dev/null -w '%{http_code}' "$B/api/me" || true)
  [ "$code" = "401" ] && break
done

curl -s -c "$TEST_HOME/jar" -H 'Content-Type: application/json' \
  -d '{"login":"ssh-e2e","password":"ssh-e2e-pass-1"}' "$B/api/register" -o /dev/null
PUB=$(cat "$TEST_HOME/id_ed25519.pub")
python3 - "$PUB" "$FAKE_PORT" <<'PY' >"$TEST_HOME/create.json"
import json, sys
pub, port = sys.argv[1:]
print(json.dumps({
  "name":"ssh-e2e", "kit":"base", "components":["core-workspace"],
  "baseURL":f"http://host.docker.internal:{port}/v1", "models":["stub-model"],
  "apiKey":"sk-e2e-fake-not-a-secret", "sshPubKey":pub
}))
PY
curl -s -b "$TEST_HOME/jar" -H 'Content-Type: application/json' \
  --data-binary @"$TEST_HOME/create.json" "$B/api/workspaces" -o "$TEST_HOME/create.out" -w '%{http_code}' | grep -q '^202$' \
  && pass "workspace provisioning accepted with sshPubKey" \
  || { fail "workspace create failed: $(cat "$TEST_HOME/create.out")"; exit 1; }

for i in $(seq 1 45); do
  sleep 2
  curl -s -b "$TEST_HOME/jar" "$B/api/workspaces" >"$TEST_HOME/list.json"
  STATE=$(python3 - "$TEST_HOME/list.json" <<'PY'
import json, sys
ws = json.load(open(sys.argv[1])).get('workspaces') or []
if ws:
  print(ws[0].get('id',''), ws[0].get('status',''), ws[0].get('sshPort',0))
PY
)
  case "$STATE" in
    *" running "*) break;;
  esac
done
WSID=$(printf '%s' "$STATE" | awk '{print $1}')
SSHPORT=$(printf '%s' "$STATE" | awk '{print $3}')
if [ -z "$WSID" ] || [ "$SSHPORT" -le 0 ]; then
  fail "workspace did not start / sshPort missing: '$STATE'"
  tail -30 "$TEST_HOME/serve.log" >&2
  exit 1
fi
pass "workspace running; sshPort=$SSHPORT"
BOUND=$(docker port "agctl-$WSID" 2222/tcp 2>/dev/null || true)
case "$BOUND" in
  127.0.0.1:*) pass "Docker SSH bind loopback-only ($BOUND)";;
  *) fail "SSH host binding is not loopback-only: '$BOUND'";;
esac

OUT=$(ssh -i "$TEST_HOME/id_ed25519" -p "$SSHPORT" \
  -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 \
  root@127.0.0.1 'printf SSH_OK' 2>"$TEST_HOME/ssh.log" || true)
[ "$OUT" = "SSH_OK" ] && pass "public-key SSH command executed in container" \
  || { fail "ssh output='$OUT'; $(tail -5 "$TEST_HOME/ssh.log" | tr '\n' ' ')"; }

PRINTED=$("$BIN" connect --host 127.0.0.1 --port "$SSHPORT" --identity "$TEST_HOME/id_ed25519" --print)
echo "$PRINTED" | grep -q "root@127.0.0.1" && pass "agctl connect renders ssh command" || fail "agctl connect output: $PRINTED"

# Rotate -> remove -> re-enable key on a running workspace; each PATCH must
# recreate the container and update the loopback port/key pair.
patch_ssh() {
  curl -s -b "$TEST_HOME/jar" -X PATCH -H 'Content-Type: application/json' \
    -d "$1" "$B/api/workspaces/$WSID/ssh" -o "$TEST_HOME/ssh-patch.json" -w '%{http_code}'
}
wait_ssh_state() {
  local want="$1" state=""
  for i in $(seq 1 45); do
    sleep 1
    curl -s -b "$TEST_HOME/jar" "$B/api/workspaces/$WSID" >"$TEST_HOME/ws.json"
    state=$(python3 - "$TEST_HOME/ws.json" <<'PY'
import json, sys
w = json.load(open(sys.argv[1])).get('workspace', {})
print(w.get('status',''), w.get('sshEnabled',False), w.get('sshPort',0))
PY
)
    case "$state" in
      "running $want "*) echo "$state"; return 0;;
    esac
  done
  echo "$state"
  return 1
}

# Removing the key disables sshd after the async container replacement.
CODE=$(patch_ssh '{"pubKey":""}')
if [ "$CODE" = "202" ]; then
  STATE=$(wait_ssh_state False) || true
  case "$STATE" in
    "running False 0") pass "SSH key removal recreated container and disabled port";;
    *) fail "SSH removal state: '$STATE'";;
  esac
else
  fail "SSH key removal PATCH: HTTP $CODE"
fi

# Enable a second key and verify it authenticates after runtime restart.
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519_rotated"
PUB2=$(cat "$TEST_HOME/id_ed25519_rotated.pub")
python3 - "$PUB2" <<'PY' >"$TEST_HOME/ssh-patch.json"
import json, sys
print(json.dumps({"pubKey": sys.argv[1]}))
PY
CODE=$(curl -s -b "$TEST_HOME/jar" -X PATCH -H 'Content-Type: application/json' \
  --data-binary @"$TEST_HOME/ssh-patch.json" "$B/api/workspaces/$WSID/ssh" \
  -o "$TEST_HOME/ssh-patch-result.json" -w '%{http_code}')
if [ "$CODE" = "202" ]; then
  STATE=$(wait_ssh_state True) || true
  SSHPORT2=$(printf '%s' "$STATE" | awk '{print $3}')
  OUT2=$(ssh -i "$TEST_HOME/id_ed25519_rotated" -p "$SSHPORT2" \
    -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 \
    root@127.0.0.1 'printf SSH_ROTATED' 2>"$TEST_HOME/ssh-rotated.log" || true)
  [ "$OUT2" = "SSH_ROTATED" ] && pass "key rotation recreated container and authenticates new key" \
    || fail "rotated-key SSH output='$OUT2'; $(tail -5 "$TEST_HOME/ssh-rotated.log" | tr '\\n' ' ')"
else
  fail "SSH key rotation PATCH: HTTP $CODE"
fi

echo "== e2e-ssh result: PASS=$PASS FAIL=$FAIL (home=$TEST_HOME) =="
[ "$FAIL" -eq 0 ]
