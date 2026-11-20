#!/bin/bash
# e2e-ssh (v3): реальный SSH-канал developer'а в персональный контейнер
# (вход только по ключу, порт на 127.0.0.1). Нужен серверный образ
# agctl-dsh-server с openssh-server (make img_build_server IMG_SERVER=...).
#
#   IMG=agctl-dsh-server:0.2.2 bin/e2e-ssh.sh
#
# Поток v3 (v2-воркспейсы и /api/workspaces удалены):
#   admin (первая учётка) + пользователь → роль user: PUT /api/me/ssh → 403 →
#   заявка на developer → апрув админом → PUT /api/me/ssh (ключ) →
#   POST /api/me/runtime/start (ensureChat) → sshPort, bind только loopback →
#   ssh по ключу → ротация (новый ключ вступает при следующем старте: stop →
#   start; старый ключ отвергается) → снятие ключа (sshd не публикуется) →
#   опционально Mutagen туда-обратно. Изолированный TEST_HOME, свои порты,
#   одноразовые ключи; приватные ключи не печатаются и удаляются в конце.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="${E2E_SSH_HOME:-/tmp/agctl-ssh-e2e-$RUN_ID}"
ADDR="127.0.0.1:${E2E_SSH_ADDR_PORT:-18932}"
B="http://$ADDR"
IMG="${IMG:-agctl-dsh-server:0.2.2}"
BIN="$TEST_HOME/agctl-server"
FAKE_PORT="${FAKE_PORT:-18770}"
ORIG_HOME="$HOME"
DEV_LOGIN="ssh-dev"; DEV_PASSWORD="ssh-e2e-pass-1"
ADMIN_LOGIN="ssh-admin"; ADMIN_PASSWORD="ssh-e2e-admin-1"

PASS=0; FAIL=0; AGCTL_PID=""; FAKE_PID=""; MUTAGEN_NAME=""; MUTAGEN_HOME=""; MUTAGEN_DAEMON_PID=""
pass() { PASS=$((PASS+1)); echo "  ok: $1"; }
fail() { FAIL=$((FAIL+1)); echo "  FAIL: $1"; }
cleanup() {
  if [ -n "$MUTAGEN_NAME" ] && [ -n "$MUTAGEN_HOME" ] && command -v mutagen >/dev/null 2>&1; then
    HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin" MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519" mutagen sync terminate "$MUTAGEN_NAME" >/dev/null 2>&1 || true
  fi
  if [ -n "$MUTAGEN_HOME" ] && command -v mutagen >/dev/null 2>&1; then
    HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin" MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519" mutagen daemon stop >/dev/null 2>&1 || true
  fi
  [ -n "$MUTAGEN_DAEMON_PID" ] && kill "$MUTAGEN_DAEMON_PID" >/dev/null 2>&1 || true
  if [ -n "$AGCTL_PID" ] && [ -f "$TEST_HOME/dev.jar" ]; then
    curl -s -b "$TEST_HOME/dev.jar" -X POST "$B/api/me/runtime/stop" -o /dev/null 2>/dev/null || true
  fi
  [ -n "$AGCTL_PID" ] && kill "$AGCTL_PID" >/dev/null 2>&1 || true
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
  # Контейнеры прогона (тома смонтированы из TEST_HOME) — убрать за собой:
  # reaper умирает вместе с serve; чужие контейнеры не трогаем.
  for cid in $(docker ps -aq --filter name=agctl-chat- 2>/dev/null); do
    mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}};{{end}}' "$cid" 2>/dev/null || true)
    case "$mounts" in
      *"$TEST_HOME"/*) docker rm -f "$cid" >/dev/null 2>&1 || true ;;
    esac
  done
  # Тестовые приватные ключи одноразовые: логи и БД оставляем, ключи удаляем.
  rm -f "$TEST_HOME/id_ed25519" "$TEST_HOME/id_ed25519.pub" \
    "$TEST_HOME/id_ed25519_rotated" "$TEST_HOME/id_ed25519_rotated.pub"
  if [ -n "$MUTAGEN_HOME" ]; then
    rm -f "$MUTAGEN_HOME/.ssh/id_ed25519" "$MUTAGEN_HOME/.ssh/config"
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "$TEST_HOME"
if [ -e "$TEST_HOME/id_ed25519" ] || [ -e "$TEST_HOME/id_ed25519_rotated" ]; then
  echo "тестовые ключи уже существуют в $TEST_HOME; выбери новый E2E_SSH_HOME" >&2
  exit 2
fi
command -v ssh-keygen >/dev/null || { echo "ssh-keygen не найден" >&2; exit 2; }
command -v ssh >/dev/null || { echo "ssh-клиент не найден" >&2; exit 2; }
docker image inspect "$IMG" >/dev/null 2>&1 || { echo "server image $IMG не найден; сначала make img_build_server IMG_SERVER=$IMG" >&2; exit 2; }

go build -o "$BIN" ./cmd/agctl-server || exit 2
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519"
python3 bin/fake-llm.py --port "$FAKE_PORT" --bind 0.0.0.0 >"$TEST_HOME/fake.log" 2>&1 &
FAKE_PID=$!
AGCTL_LLM_API_KEY=e2e-fake-key-not-a-secret "$BIN" serve --home "$TEST_HOME/data" --addr "$ADDR" --image "$IMG" \
  --llm-base-url "http://host.docker.internal:$FAKE_PORT/v1" \
  --llm-model stub-model --llm-models stub-model >"$TEST_HOME/serve.log" 2>&1 &
AGCTL_PID=$!
READY=""
for i in $(seq 1 30); do
  sleep 1
  code=$(curl -s -o /dev/null -w '%{http_code}' "$B/api/me" || true)
  [ "$code" = "401" ] && READY=1 && break
done
[ -n "$READY" ] || { echo "agctl-server не стартовал; лог: $TEST_HOME/serve.log" >&2; tail -25 "$TEST_HOME/serve.log" >&2; exit 1; }

# api <jar> <METHOD> <path> [json] — тело ответа в $TEST_HOME/last.json, печатает HTTP-код.
api() {
  local jar="$1" m="$2" p="$3" d="${4:-}"
  local args=(-s -b "$jar" -c "$jar" -X "$m" -o "$TEST_HOME/last.json" -w '%{http_code}')
  [ -n "$d" ] && args+=(-H 'Content-Type: application/json' --data-binary "$d")
  curl "${args[@]}" "$B$p"
}
jlast() { python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print('"$1"')' "$TEST_HOME/last.json" 2>/dev/null; }
put_key() { # <файл .pub или пусто> — PUT /api/me/ssh от developer'а
  local body
  body=$(python3 -c 'import json,sys; print(json.dumps({"pubKey": open(sys.argv[1]).read().strip() if sys.argv[1] else ""}))' "${1:-}")
  api "$TEST_HOME/dev.jar" PUT /api/me/ssh "$body"
}
start_runtime() { # печатает sshPort живого контейнера
  local code
  code=$(api "$TEST_HOME/dev.jar" POST /api/me/runtime/start)
  [ "$code" = 200 ] || { echo "start:$code:$(head -c 300 "$TEST_HOME/last.json")"; return 1; }
  jlast 'd.get("sshPort",0)'
}
stop_runtime() { api "$TEST_HOME/dev.jar" POST /api/me/runtime/stop; }
ssh_run() { # <ключ> <порт> <команда>
  ssh -i "$1" -p "$2" -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null -o ConnectTimeout=8 root@127.0.0.1 "$3"
}
wait_gone() { # <имя контейнера> — StopChat удаляет compute асинхронно
  for i in $(seq 1 60); do
    [ -z "$(docker ps -aq --filter name="^/$1$")" ] && return 0
    sleep 1
  done
  return 1
}
dev_container() {
  api "$TEST_HOME/dev.jar" GET /api/me/containers >/dev/null
  jlast '(d.get("containers") or [{}])[0].get("name","")'
}

echo "== Роли: admin + developer по заявке =="
code=$(api "$TEST_HOME/admin.jar" POST /api/register "{\"login\":\"$ADMIN_LOGIN\",\"password\":\"$ADMIN_PASSWORD\"}")
[ "$code" = 200 ] || [ "$code" = 201 ] || { fail "register admin: $code"; exit 1; }
code=$(api "$TEST_HOME/dev.jar" POST /api/register "{\"login\":\"$DEV_LOGIN\",\"password\":\"$DEV_PASSWORD\"}")
[ "$code" = 200 ] || [ "$code" = 201 ] || { fail "register dev: $code"; exit 1; }
code=$(put_key "$TEST_HOME/id_ed25519.pub")
[ "$code" = 403 ] && pass "роль user: PUT /api/me/ssh → 403 (SSH только developer)" || fail "роль user PUT /api/me/ssh: $code"
code=$(api "$TEST_HOME/dev.jar" POST /api/role-requests '{"motivation":"e2e-ssh"}')
[ "$code" = 200 ] || { fail "заявка на developer: $code $(cat "$TEST_HOME/last.json")"; exit 1; }
RID=$(jlast 'd["request"]["id"]')
code=$(api "$TEST_HOME/admin.jar" POST "/api/role-requests/$RID/approve")
[ "$code" = 200 ] && pass "заявка $RID одобрена админом" || { fail "approve: $code $(cat "$TEST_HOME/last.json")"; exit 1; }
api "$TEST_HOME/dev.jar" GET /api/me >/dev/null
[ "$(jlast 'd["user"]["role"]')" = developer ] && pass "роль developer привязана" || fail "роль после апрува: $(jlast 'd["user"]["role"]')"

echo "== Ключ → старт контейнера → SSH =="
code=$(put_key "$TEST_HOME/id_ed25519.pub")
[ "$code" = 200 ] && pass "PUT /api/me/ssh: ключ сохранён" || fail "PUT /api/me/ssh: $code $(cat "$TEST_HOME/last.json")"
api "$TEST_HOME/dev.jar" GET /api/me/ssh >/dev/null
case "$(jlast 'd.get("pubKey","")')" in
  ssh-ed25519\ *) pass "GET /api/me/ssh отдаёт сохранённый ключ";;
  *) fail "GET /api/me/ssh: pubKey не сохранён";;
esac
SSHPORT=$(start_runtime) || true
case "$SSHPORT" in
  ''|0|start:*) fail "контейнер без sshPort: '$SSHPORT'"; tail -30 "$TEST_HOME/serve.log" >&2; exit 1;;
esac
pass "POST /api/me/runtime/start: running, sshPort=$SSHPORT"
api "$TEST_HOME/dev.jar" GET /api/me/ssh >/dev/null
[ "$(jlast 'd.get("sshPort",0)')" = "$SSHPORT" ] && pass "GET /api/me/ssh: живой порт совпадает" || fail "GET /api/me/ssh sshPort=$(jlast 'd.get("sshPort",0)')"
CONTAINER=$(dev_container)
BOUND=$(docker port "$CONTAINER" 2222/tcp 2>/dev/null || true)
case "$BOUND" in
  127.0.0.1:*) pass "SSH опубликован только на loopback ($BOUND)";;
  *) fail "SSH-порт не только на loopback: '$BOUND' (контейнер '$CONTAINER')";;
esac
OUT=$(ssh_run "$TEST_HOME/id_ed25519" "$SSHPORT" 'printf SSH_OK' 2>"$TEST_HOME/ssh.log" || true)
[ "$OUT" = "SSH_OK" ] && pass "вход по ключу, команда выполнена в контейнере" \
  || fail "ssh output='$OUT'; $(tail -5 "$TEST_HOME/ssh.log" | tr '\n' ' ')"
PRINTED=$("$BIN" connect --host 127.0.0.1 --port "$SSHPORT" --identity "$TEST_HOME/id_ed25519" --print)
echo "$PRINTED" | grep -q "root@127.0.0.1" && pass "agctl-server connect --print рисует ssh-команду" || fail "connect --print: $PRINTED"

echo "== Ротация ключа (вступает при следующем старте) =="
ssh-keygen -q -t ed25519 -N '' -f "$TEST_HOME/id_ed25519_rotated"
code=$(put_key "$TEST_HOME/id_ed25519_rotated.pub")
[ "$code" = 200 ] && pass "PUT /api/me/ssh: новый ключ сохранён" || fail "ротация PUT: $code"
code=$(stop_runtime)
[ "$code" = 200 ] && wait_gone "$CONTAINER" && pass "stop: compute удалён, том сохранён" || fail "stop runtime: $code / контейнер не удалён"
SSHPORT2=$(start_runtime) || true
case "$SSHPORT2" in
  ''|0|start:*) fail "после ротации контейнер без sshPort: '$SSHPORT2'"; SSHPORT2=0;;
  *)
    OUT2=$(ssh_run "$TEST_HOME/id_ed25519_rotated" "$SSHPORT2" 'printf SSH_ROTATED' 2>"$TEST_HOME/ssh-rotated.log" || true)
    [ "$OUT2" = "SSH_ROTATED" ] && pass "новый ключ проходит после рестарта" \
      || fail "rotated-key ssh output='$OUT2'; $(tail -5 "$TEST_HOME/ssh-rotated.log" | tr '\n' ' ')"
    OLD=$(ssh_run "$TEST_HOME/id_ed25519" "$SSHPORT2" 'printf OLD_KEY_IN' 2>/dev/null || true)
    [ -z "$OLD" ] && pass "старый ключ отвергнут" || fail "старый ключ всё ещё пускает"
    ;;
esac

echo "== Mutagen (опционально) =="
# Optional real Mutagen round-trip. If mutagen is absent, earlier SSH checks
# still pass and this phase is reported as SKIP. Set MUTAGEN_HOME so the
# temporary test daemon/config/identity never touch the user's real profile.
if [ "$SSHPORT2" -gt 0 ] && command -v mutagen >/dev/null 2>&1; then
  MUTAGEN_HOME="$TEST_HOME/mutagen-home"
  MUTAGEN_NAME="agctl-e2e-$RUN_ID"
  LOCAL_SYNC="$TEST_HOME/local-sync"
  mkdir -p "$MUTAGEN_HOME/.ssh" "$LOCAL_SYNC"
  cp "$TEST_HOME/id_ed25519_rotated" "$MUTAGEN_HOME/.ssh/id_ed25519"
  chmod 700 "$MUTAGEN_HOME/.ssh"
  chmod 600 "$MUTAGEN_HOME/.ssh/id_ed25519"
  mkdir -p "$MUTAGEN_HOME/bin"
  cat >"$MUTAGEN_HOME/bin/ssh" <<'SSHWRAP'
#!/bin/sh
exec /usr/bin/ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$MUTAGEN_E2E_KEY" "$@"
SSHWRAP
  cat >"$MUTAGEN_HOME/bin/scp" <<'SCPWRAP'
#!/bin/sh
exec /usr/bin/scp -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$MUTAGEN_E2E_KEY" "$@"
SCPWRAP
  chmod 700 "$MUTAGEN_HOME/bin/ssh" "$MUTAGEN_HOME/bin/scp"
  export HOME="$MUTAGEN_HOME"
  export MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data"
  export MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin"
  export MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519"
  printf 'local-sync-probe\n' >"$LOCAL_SYNC/local-probe.txt"
  HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" \
    MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin" MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519" \
    mutagen daemon run >"$TEST_HOME/mutagen-daemon.log" 2>&1 &
  MUTAGEN_DAEMON_PID=$!
  DAEMON_READY=""
  for i in $(seq 1 20); do
    if HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" \
      mutagen sync list >/dev/null 2>&1; then DAEMON_READY=1; break; fi
    sleep 1
  done
  if [ -z "$DAEMON_READY" ]; then
    fail "Mutagen daemon did not start: $(tail -5 "$TEST_HOME/mutagen-daemon.log" | tr '\\n' ' ')"
  elif HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" \
    mutagen sync create --name "$MUTAGEN_NAME" \
    --sync-mode two-way-safe --ignore-vcs --ignore node_modules --ignore .env \
    --ignore '.env.*' --ignore .credentials.yaml --ignore .credentials.yml --ignore .ssh \
    "$LOCAL_SYNC" "root@127.0.0.1:$SSHPORT2:/workspace" >"$TEST_HOME/mutagen-create.log" 2>&1; then
    REMOTE=""
    for i in $(seq 1 45); do
      sleep 1
      REMOTE=$(ssh -i "$TEST_HOME/id_ed25519_rotated" -p "$SSHPORT2" \
        -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null -o ConnectTimeout=5 root@127.0.0.1 \
        'cat /workspace/local-probe.txt 2>/dev/null' 2>/dev/null || true)
      [ "$REMOTE" = "local-sync-probe" ] && break
    done
    if [ "$REMOTE" = "local-sync-probe" ]; then
      pass "Mutagen sync pushed local file to workspace"
      ssh -i "$TEST_HOME/id_ed25519_rotated" -p "$SSHPORT2" \
        -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=no \
        -o UserKnownHostsFile=/dev/null root@127.0.0.1 \
        "printf 'remote-sync-probe\\n' > /workspace/remote-probe.txt"
      for i in $(seq 1 45); do
        sleep 1
        [ "$(cat "$LOCAL_SYNC/remote-probe.txt" 2>/dev/null || true)" = "remote-sync-probe" ] && break
      done
      [ "$(cat "$LOCAL_SYNC/remote-probe.txt" 2>/dev/null || true)" = "remote-sync-probe" ] \
        && pass "Mutagen sync pulled remote file to local folder" \
        || fail "Mutagen did not pull remote file; log=$TEST_HOME/mutagen-create.log"
    else
      fail "Mutagen did not push local file; log=$TEST_HOME/mutagen-create.log"
    fi
  else
    fail "Mutagen sync create failed: $(tail -5 "$TEST_HOME/mutagen-create.log" | tr '\\n' ' ')"
  fi
  HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin" MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519" mutagen sync terminate "$MUTAGEN_NAME" >/dev/null 2>&1 || true
  HOME="$MUTAGEN_HOME" MUTAGEN_DATA_DIRECTORY="$MUTAGEN_HOME/data" MUTAGEN_SSH_PATH="$MUTAGEN_HOME/bin" MUTAGEN_E2E_KEY="$MUTAGEN_HOME/.ssh/id_ed25519" mutagen daemon stop >/dev/null 2>&1 || true
  MUTAGEN_NAME=""
  # Дальше снова docker/ssh пользователя: вернуть HOME (контекст Docker в ~/.docker).
  export HOME="$ORIG_HOME"
  unset MUTAGEN_DATA_DIRECTORY MUTAGEN_SSH_PATH MUTAGEN_E2E_KEY
else
  echo "  SKIP: Mutagen CLI не установлен (или нет живого SSH после ротации)"
fi


echo "== Снятие ключа: sshd не публикуется =="
code=$(put_key "")
[ "$code" = 200 ] && pass "PUT /api/me/ssh с пустым ключом: снят" || fail "снятие ключа: $code"
CONTAINER=$(dev_container)
code=$(stop_runtime)
[ "$code" = 200 ] && wait_gone "$CONTAINER" || fail "stop перед снятием: $code"
SSHPORT3=$(start_runtime) || true
case "$SSHPORT3" in
  0) pass "без ключа контейнер стартует без SSH-порта";;
  *) fail "после снятия ключа sshPort='$SSHPORT3'";;
esac

echo "== e2e-ssh result: PASS=$PASS FAIL=$FAIL (home=$TEST_HOME) =="
[ "$FAIL" -eq 0 ]
