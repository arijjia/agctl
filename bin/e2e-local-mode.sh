#!/bin/bash
# e2e-local-mode: живой прогон локального режима (agctl dev) на изолированном
# доме: init → secrets → up → вход по токену → рабочее место с родной папкой
# и env доступов → мост к хосту (вызов, подтверждение, MCP) → update на другой
# тег с бэкапом → rollback → down. Секреты — в файловом хранилище (хранилище ОС
# не трогаем), значения тестовые. Образ должен быть собран (make img_build).
#
#   IMG=agctl-dsh:0.2.2 bin/e2e-local-mode.sh
#
# Переменные: IMG (образ), AGCTL_BIN (бинарь, по умолчанию dist/agctl),
# E2E_ADDR (адрес UI, по умолчанию 127.0.0.1:18940), KEEP=1 — не чистить дом.
set -u
cd "$(dirname "$0")/.."
IMG="${IMG:-agctl-dsh:0.2.2}"
BIN="${AGCTL_BIN:-dist/agctl}"
ADDR="${E2E_ADDR:-127.0.0.1:18940}"
PASS=0; FAIL=0
pass() { PASS=$((PASS + 1)); printf '  PASS: %s\n' "$*"; }
fail() { FAIL=$((FAIL + 1)); printf '  FAIL: %s\n' "$*"; }
say()  { printf '\n=== %s ===\n' "$*"; }

[ -x "$BIN" ] || { echo "нет $BIN — make go_build"; exit 2; }
docker image inspect "$IMG" >/dev/null 2>&1 || { echo "нет образа $IMG — make img_build"; exit 2; }

RUN="$(cd "$(mktemp -d "${TMPDIR:-/tmp}/agctl-e2e-local.XXXXXX")" && pwd -P)"  # канонический путь: маунты Docker его и показывают
export AGCTL_DEV_HOME="$RUN/home"
export AGCTL_DEV_SECRETS=file
WORK="$RUN/work"
mkdir -p "$WORK/demo-repo" && echo "hello" > "$WORK/demo-repo/README.md"
IMG_B="${IMG%%:*}:e2e-local-b"
docker tag "$IMG" "$IMG_B"

cleanup() {
  "$BIN" dev down >/dev/null 2>&1
  # контейнеры этого прогона (маунт из $RUN)
  for c in $(docker ps -aq); do
    if docker inspect "$c" --format '{{range .Mounts}}{{.Source}} {{end}}' 2>/dev/null | grep -q "$RUN"; then
      docker rm -f "$c" >/dev/null 2>&1
    fi
  done
  docker rmi "$IMG_B" >/dev/null 2>&1
  if [ "${KEEP:-0}" != 1 ] && [ -n "$RUN" ] && [ -d "$RUN" ]; then
    rm -rf -- "$RUN"
  fi
}
trap cleanup EXIT

say "init (без вопросов) и доступы"
"$BIN" dev init --yes --no-pull --profile agctl --image "$IMG" --workdir "$WORK" --addr "$ADDR" >/dev/null \
  && pass "dev init" || fail "dev init"
# адрес UI и белый список моста (go_env — бинарь go есть у разработчика платформы;
# echo_note — с подтверждением)
grep -q '^bridge:' "$AGCTL_DEV_HOME/dev.yaml" && { echo "dev.yaml уже с bridge"; exit 2; }
cat >> "$AGCTL_DEV_HOME/dev.yaml" <<EOF
bridge:
  tools:
    - name: host_uname
      description: имя ОС хоста
      command: ["go", "env", "GOOS"]
    - name: host_echo
      description: эхо с подтверждением
      command: ["go", "env", "{{name}}"]
      args:
        name: {required: true, pattern: "[A-Z]+"}
      confirm: true
EOF
printf 'e2e-gitlab-token\n' | "$BIN" dev secrets set GITLAB_TOKEN --stdin >/dev/null && pass "secret GITLAB_TOKEN" || fail "secret"
printf 'https://gitlab.example.org\n' | "$BIN" dev secrets set GITLAB_URL --stdin >/dev/null && pass "GITLAB_URL (несекретная)" || fail "GITLAB_URL"
grep -q "e2e-gitlab-token" "$AGCTL_DEV_HOME/dev.yaml" && fail "секрет утёк в dev.yaml" || pass "секрета нет в dev.yaml"
grep -q "e2e-gitlab-token" "$AGCTL_DEV_HOME/secrets.enc" 2>/dev/null && fail "секрет в открытом виде" || pass "secrets.enc зашифрован"

say "up"
"$BIN" dev up --no-open >"$RUN/up.log" 2>&1 && pass "dev up" || { fail "dev up"; cat "$RUN/up.log"; tail -30 "$AGCTL_DEV_HOME/logs/serve.log"; exit 1; }
STATE="$AGCTL_DEV_HOME/run/state.json"
LOGIN=$(sed -n 's/.*"loginToken": "\([^"]*\)".*/\1/p' "$STATE")
CTL=$(sed -n 's/.*"controlToken": "\([^"]*\)".*/\1/p' "$STATE")
[ -n "$LOGIN" ] && [ -n "$CTL" ] && pass "файл состояния с токенами" || fail "state.json"
perm=$(stat -f '%Lp' "$STATE" 2>/dev/null || stat -c '%a' "$STATE")
[ "$perm" = "600" ] && pass "state.json 0600" || fail "state.json права $perm"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' \
  -d '{"login":"x","password":"Password-123!"}' "http://$ADDR/api/register")
[ "$code" = "403" ] && pass "регистрации нет (403)" || fail "register: $code"
code=$(curl -s -o /dev/null -w '%{http_code}' "http://$ADDR/api/local/status")
[ "$code" = "401" ] && pass "status без токена — 401" || fail "status без токена: $code"

say "вход по токену и рабочее место"
JAR="$RUN/jar"
code=$(curl -s -c "$JAR" -o /dev/null -w '%{http_code}' "http://$ADDR/local/login?token=$LOGIN")
[ "$code" = "303" ] && pass "вход владельца (303)" || fail "login: $code"
me=$(curl -s -b "$JAR" "http://$ADDR/api/me")
echo "$me" | grep -q '"role":"developer"' && pass "владелец — developer" || fail "me: $me"
CID=""
for i in $(seq 1 90); do
  st=$(curl -s -H "Authorization: Bearer $CTL" "http://$ADDR/api/local/status")
  echo "$st" | grep -q '"state":"running"' && break
  sleep 2
done
echo "$st" | grep -q '"state":"running"' && pass "рабочее место поднято" || { fail "runtime: $st"; tail -20 "$AGCTL_DEV_HOME/logs/serve.log"; }
for c in $(docker ps -q); do
  if docker inspect "$c" --format '{{range .Mounts}}{{.Source}} {{end}}' | grep -q "$RUN"; then CID=$c; fi
done
[ -n "$CID" ] && pass "контейнер найден" || fail "контейнер не найден"
if [ -n "$CID" ]; then
  docker exec "$CID" cat /workspace/demo-repo/README.md 2>/dev/null | grep -q hello \
    && pass "/workspace = родная папка" || fail "маунт папки"
  docker exec "$CID" sh -c 'echo "$GITLAB_TOKEN|$GITLAB_HOST|$GITLAB_API_URL"' | grep -q "e2e-gitlab-token|gitlab.example.org|https://gitlab.example.org/api/v4" \
    && pass "env доступов + производные имена" || fail "env доступов"
  docker exec "$CID" sh -c 'test -z "${AGCTL_LLM_API_KEY:-}"' && pass "ключа LLM нет в env контейнера" || fail "ключ LLM в env"
  docker exec "$CID" sh -c 'glab --version >/dev/null && jira version >/dev/null && uv --version >/dev/null && rg --version >/dev/null && jq --version >/dev/null' \
    && pass "инструменты: glab, jira, uv, rg, jq" || fail "инструменты образа"

  say "доступы для команд агента (bash dsh вычищает *TOKEN* из env)"
  docker exec "$CID" sh -c 'git config --global credential.helper' | grep -q "store --file=/home/dsh/.agctl/git-credentials" \
    && pass "git: credential helper из dsh-home" || fail "git credential helper"
  docker exec "$CID" sh -c 'test -f "$GLAB_CONFIG_DIR/config.yml" && grep -q gitlab.example.org "$GLAB_CONFIG_DIR/config.yml"' \
    && pass "glab: конфиг хоста" || fail "glab config"
  docker exec "$CID" sh -c 'set -a; . "$AGCTL_CREDENTIALS_ENV"; set +a; test "$GITLAB_TOKEN" = e2e-gitlab-token' \
    && pass "файл доступов подгружается" || fail "credentials.env"
  docker exec "$CID" sh -c 'ls -a /workspace | grep -qE "^\.(netrc|git-credentials|gitconfig)$"' \
    && fail "доступы утекли в /workspace" || pass "в /workspace (папка разработчика) доступов нет"
  out=$(docker exec -e AGCTL_BRIDGE_URL= -e AGCTL_BRIDGE_TOKEN= "$CID" agctl bridge list 2>&1)
  echo "$out" | grep -q host_uname && pass "мост без env (как у агента) — из bridge.json" || fail "bridge без env: $out"

  say "мост к хосту"
  out=$(docker exec "$CID" agctl bridge list 2>&1)
  echo "$out" | grep -q host_uname && ! echo "$out" | grep -q '"command"' && pass "bridge list (без argv)" || fail "bridge list: $out"
  out=$(docker exec "$CID" agctl bridge call host_uname 2>&1)
  echo "$out" | grep -q '"exitCode": 0' && echo "$out" | grep -qi "$(go env GOOS)" && pass "bridge call исполнился на хосте" || fail "bridge call: $out"
  out=$(docker exec "$CID" agctl bridge call rm_rf 2>&1)
  echo "$out" | grep -q "белом списке" && pass "вне белого списка — отказ" || fail "white list: $out"
  out=$(docker exec "$CID" agctl bridge call host_echo "name=GOOS; ls" 2>&1)
  echo "$out" | grep -q "проверку" && pass "значение не по pattern — отказ" || fail "pattern: $out"
  ( docker exec "$CID" agctl bridge call host_echo name=GOARCH > "$RUN/confirm.out" 2>&1 ) &
  CALLPID=$!
  PID_ID=""
  for i in $(seq 1 30); do
    PID_ID=$("$BIN" dev bridge pending 2>/dev/null | awk '/host_echo/{print $1; exit}')
    [ -n "$PID_ID" ] && break
    sleep 0.5
  done
  [ -n "$PID_ID" ] && pass "вызов ждёт подтверждения" || fail "нет pending"
  "$BIN" dev bridge approve "$PID_ID" >/dev/null 2>&1
  wait $CALLPID
  grep -q '"exitCode": 0' "$RUN/confirm.out" && pass "подтверждённый вызов исполнился" || fail "confirm: $(cat "$RUN/confirm.out")"
  mcp=$(printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
    | docker exec -i "$CID" agctl bridge mcp 2>&1)
  echo "$mcp" | grep -q '"agctl-host-bridge"' && echo "$mcp" | grep -q host_uname && pass "MCP моста: initialize + tools/list" || fail "mcp: $mcp"
  test -f "$AGCTL_DEV_HOME/data/chats/u1/dsh-home/skills/agctl-host-bridge/SKILL.md" && pass "скилл моста в dsh-home" || fail "скилл"
  grep -q '"tool":"host_uname"' "$AGCTL_DEV_HOME/logs/bridge.log" && pass "журнал моста" || fail "журнал"
fi
"$BIN" dev status | grep -q "запущено" && pass "dev status" || fail "dev status"

say "update на другой тег (бэкап) и rollback"
"$BIN" dev update --image "$IMG_B" >"$RUN/update.log" 2>&1 && pass "dev update" || { fail "dev update"; cat "$RUN/update.log"; }
ls "$AGCTL_DEV_HOME"/backups/dsh-home-*.tar.gz >/dev/null 2>&1 && pass "бэкап dsh-home создан" || fail "бэкап"
grep -q "image: $IMG_B" "$AGCTL_DEV_HOME/dev.yaml" && grep -q "previous_image: $IMG" "$AGCTL_DEV_HOME/dev.yaml" \
  && pass "dev.yaml: новый образ + предыдущий" || fail "dev.yaml после update"
"$BIN" dev rollback >"$RUN/rollback.log" 2>&1 && pass "dev rollback" || { fail "rollback"; cat "$RUN/rollback.log"; }
grep -q "^image: $IMG\$" "$AGCTL_DEV_HOME/dev.yaml" && pass "образ возвращён" || fail "образ после rollback"
"$BIN" dev status | grep -q "запущено" && pass "после rollback запущено" || fail "status после rollback"

say "down"
"$BIN" dev down >/dev/null && pass "dev down" || fail "dev down"
[ ! -f "$STATE" ] && pass "файл состояния убран" || fail "state.json остался"
test -f "$WORK/demo-repo/README.md" && pass "папка разработчика цела" || fail "папка"

printf '\n=== ИТОГО: PASS=%d FAIL=%d ===\n' "$PASS" "$FAIL"
[ "$FAIL" -eq 0 ]
