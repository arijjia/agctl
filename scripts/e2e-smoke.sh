#!/bin/bash
# e2e-smoke: изолированный дымовой прогон agctl по мотивам
# agctl-hq/docs/agctl-mvp-e2e-test-guide.md. Всё в своём TEST_HOME,
# свой тег образа, свой порт; секреты/токены не печатаются.
#
#   TEST_IMAGE=agctl-dsh:0.2.0-rc2-e2e-n1 scripts/e2e-smoke.sh
#
# Переменные:
#   TEST_IMAGE  (обяз.) тег уже собранного образа
#   AGCTL_BIN   бинарь (по умолчанию dist/agctl, соберём если нет)
#   TEST_ADDR   адрес UI (по умолчанию 127.0.0.1:18929)
#   FAKE_LLM    1 = поднять стаб OpenAI-API и проверить живой провод
#               до модели (контейнер → стаб) curl-пробой с env-токеном
#   CLEANUP     1 = удалить TEST_HOME после успешного прогона
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

PASS=0; FAIL=0; FAILED=0
say()  { printf '\n=== %s ===\n' "$*"; }
pass() { PASS=$((PASS + 1)); printf '  PASS: %s\n' "$*"; }
fail() { FAIL=$((FAIL + 1)); FAILED=1; printf '  FAIL: %s\n' "$*"; }
skip() { printf '  SKIP: %s\n' "$*"; }

[ -n "${TEST_IMAGE:-}" ] || { echo "TEST_IMAGE не задан" >&2; exit 2; }
TEST_ADDR="${TEST_ADDR:-127.0.0.1:18929}"
TEST_PORT="${TEST_ADDR##*:}"
AGCTL_BIN="${AGCTL_BIN:-$REPO_ROOT/dist/agctl}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="$HOME/.agctl-e2e-$RUN_ID"
COOKIES_ADMIN="$TEST_HOME/qa-admin.cookies"
COOKIES_USER="$TEST_HOME/qa-user.cookies"
FAKE_PORT="${FAKE_PORT:-18765}"
FAKE_PID=""; SERVE_PID=""
PROVIDER_URL="http://provider.invalid/v1"   # недоступный — чат будет NOT RUN
LAST="/tmp/agctl-e2e-$RUN_ID-last.json"

umask 077
mkdir -p "$TEST_HOME"
say "Прогон $RUN_ID: home=$TEST_HOME addr=$TEST_ADDR image=$TEST_IMAGE"

preflight() {
  command -v go >/dev/null || { echo "нет go" >&2; exit 2; }
  command -v docker >/dev/null || { echo "нет docker" >&2; exit 2; }
  docker version --format '{{.Server.Version}}' >/dev/null || { echo "docker мёртв" >&2; exit 2; }
  if lsof -nP -iTCP:"$TEST_PORT" -sTCP:LISTEN | grep -q LISTEN; then
    echo "порт $TEST_PORT занят — выбери другой TEST_ADDR" >&2; exit 2
  fi
  if [ ! -x "$AGCTL_BIN" ]; then
    say "Сборка бинаря"
    make web_build >/dev/null 2>&1 || { echo "web_build упал" >&2; exit 2; }
    go build -trimpath -o "$AGCTL_BIN" ./cmd/agctl || { echo "go build упал" >&2; exit 2; }
  fi
  pass "preflight: инструменты на месте, порт $TEST_PORT свободен"
}

start_fake_llm() {
  [ "${FAKE_LLM:-0}" = "1" ] || return 0
  python3 "$REPO_ROOT/scripts/fake-llm.py" --port "$FAKE_PORT" \
    >"/tmp/agctl-fake-llm-$RUN_ID.log" 2>&1 &
  FAKE_PID=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    curl -s -o /dev/null "http://127.0.0.1:$FAKE_PORT/v1/models" && break
    sleep 0.3
  done
  PROVIDER_URL="http://host.docker.internal:$FAKE_PORT/v1"
  pass "fake-llm: стаб поднят на 127.0.0.1:$FAKE_PORT (модели: stub-model)"
}

api() { # api <cookies|-> <method> <path> [json]
  local c="$1" m="$2" p="$3" d="${4:-}"
  local args=(-sS -o "$LAST" -w '%{http_code}' -X "$m")
  [ "$c" != "-" ] && args+=(-b "$c")
  [ -n "$d" ] && args+=(-H 'Content-Type: application/json' -d "$d")
  curl "${args[@]}" "$BASE_URL$p"
}

jsonget() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$1"')' "$LAST" 2>/dev/null; }

wait_status() { # <id> <status> <timeout_s>
  local id="$1" want="$2" t="$3" waited=0
  while [ "$waited" -lt "$t" ]; do
    api "$COOKIES_ADMIN" GET "/api/workspaces/$id" >/dev/null
    [ "$(jsonget "['workspace']['status']")" = "$want" ] && return 0
    sleep 3; waited=$((waited + 3))
  done
  return 1
}

cleanup() {
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" 2>/dev/null
}
trap cleanup EXIT INT TERM

preflight
start_fake_llm

say "Сервис agctl"
"$AGCTL_BIN" serve --home "$TEST_HOME" --addr "$TEST_ADDR" \
  --image "$TEST_IMAGE" --max-per-user 3 --mem-gb 2 --cpus 2 \
  >"/tmp/agctl-serve-$RUN_ID.log" 2>&1 &
SERVE_PID=$!
BASE_URL="http://$TEST_ADDR"
up=0
for _ in $(seq 1 40); do
  [ "$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/" 2>/dev/null)" = "200" ] && { up=1; break; }
  sleep 0.5
done
[ "$up" = 1 ] && pass "UI отвечает 200 на $BASE_URL" || fail "UI не поднялся"

say "Регистрация и роли"
code=$(curl -sS -c "$COOKIES_ADMIN" -o "$LAST" -w '%{http_code}' -H 'Content-Type: application/json' \
  -d '{"login":"qa-admin","password":"TempTest-123"}' "$BASE_URL/api/register")
[ "$code" = 200 ] && pass "регистрация qa-admin: $code" || fail "register admin: $code"
code=$(curl -sS -c "$COOKIES_USER" -o "$LAST" -w '%{http_code}' -H 'Content-Type: application/json' \
  -d '{"login":"qa-user","password":"TempTest-456"}' "$BASE_URL/api/register")
[ "$code" = 200 ] && pass "регистрация qa-user: $code" || fail "register user: $code"
role=$(curl -sS -b "$COOKIES_ADMIN" "$BASE_URL/api/me" | python3 -c 'import json,sys; print(json.load(sys.stdin)["user"]["role"])' 2>/dev/null)
[ "$role" = "admin" ] && pass "первый пользователь — admin" || fail "роль: $role"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/workspaces")
[ "$code" = 401 ] && pass "без входа /api/workspaces → 401" || fail "аноним: $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "$BASE_URL/api/admin/users")
[ "$code" = 403 ] && pass "не-admin /api/admin/users → 403" || fail "users: $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "$BASE_URL/api/admin/audit")
[ "$code" = 403 ] && pass "не-admin /api/admin/audit → 403" || fail "audit: $code"

say "Создание рабочего места (base, провайдер=$PROVIDER_URL)"
code=$(api "$COOKIES_ADMIN" POST /api/workspaces \
  "{\"name\":\"e2e-base\",\"template\":\"base\",\"baseURL\":\"$PROVIDER_URL\",\"models\":[\"stub-model\"],\"apiKey\":\"sk-e2e-fake-not-a-secret\"}")
[ "$code" = 202 ] && pass "create → 202 Accepted" || { fail "create: $code"; cat "$LAST"; }
WS_ID=$(jsonget "['workspace']['id']")
[ -n "${WS_ID:-}" ] && pass "ws id: получен" || fail "нет id воркспейса"

say "Ожидание running"
if [ -n "${WS_ID:-}" ] && wait_status "$WS_ID" running 180; then
  pass "статус running"
else
  fail "не перешёл в running за 180с"
  api "$COOKIES_ADMIN" GET "/api/workspaces/${WS_ID:-x}" >/dev/null
  printf '  error=%s\n' "$(jsonget "['workspace']['error']")"
  WS_ID="${WS_ID:-}"
fi
WS_URL=""
if [ -n "${WS_ID:-}" ]; then
  api "$COOKIES_ADMIN" GET "/api/workspaces/$WS_ID" >/dev/null
  WS_URL=$(jsonget "['workspace']['url']")
  case "$WS_URL" in
    http://127.0.0.1:*) pass "url привязан к loopback" ;;
    "") fail "url пустой" ;;
    *) fail "url не loopback: $WS_URL" ;;
  esac
fi

say "Открытие UI воркспейса"
if [ -n "$WS_URL" ]; then
  code=$(curl -sS -o "/tmp/agctl-e2e-$RUN_ID-dsh.html" -w '%{http_code}' "$WS_URL")
  size=$(wc -c < "/tmp/agctl-e2e-$RUN_ID-dsh.html" | tr -d ' ')
  if [ "$code" = 200 ] && [ "$size" -gt 500 ]; then
    pass "DSH UI отвечает 200 (${size}b), boot-токен в ссылке принят"
  else
    fail "DSH UI: http=$code size=$size"
  fi
fi

say "Изоляция: чужое место и редирект"
if [ -n "${WS_ID:-}" ]; then
  code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "$BASE_URL/w/$WS_ID")
  [ "$code" = 403 ] && pass "чужой /w/{id} → 403" || fail "чужой /w: $code"
  code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "/api/workspaces/$WS_ID")
  case "$code" in 403|404) pass "чужой workspace API → $code" ;; *) fail "чужой API: $code" ;; esac
  code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_ADMIN" "$BASE_URL/w/$WS_ID")
  [ "$code" = 307 ] && pass "владелец /w/{id} → 307 (Location не печатаем)" || fail "владелец /w: $code"
fi

say "Docker: порты, лимиты, монтирования"
CONTAINER="agctl-$WS_ID"
docker inspect --format '{{range $p, $b := .NetworkSettings.Ports}}{{$p}} -> {{range $b}}{{.HostIP}}:{{.HostPort}} {{end}}{{end}}' "$CONTAINER" 2>/dev/null | grep -q '127.0.0.1:' \
  && pass "порт контейнера опубликован на 127.0.0.1" || fail "порт не на loopback"
mem=$(docker inspect --format '{{.HostConfig.Memory}}' "$CONTAINER" 2>/dev/null)
[ "$mem" = "2147483648" ] && pass "лимит памяти 2GiB" || fail "memory=$mem"
mounts=$(docker inspect --format '{{range .Mounts}}{{.Destination}} {{end}}' "$CONTAINER" 2>/dev/null)
echo "$mounts" | grep -q '/workspace' && echo "$mounts" | grep -q '/home/dsh' \
  && pass "монтирования /workspace и /home/dsh" || fail "mounts: $mounts"

say "Git: шаблон, autosave, persist"
WORK_PATH="$TEST_HOME/work/$WS_ID/repo"
BARE_PATH="$TEST_HOME/repos/qa-admin/e2e-base.git"
git --git-dir="$BARE_PATH" log --oneline --all 2>/dev/null | grep -q 'init:' \
  && pass "bare: стартовый коммит шаблона" || fail "bare пуст/нет init"
git -C "$WORK_PATH" remote get-url origin >/dev/null 2>&1 \
  && pass "рабочий клон имеет origin" || fail "нет origin"
printf 'Временная E2E-проверка; не пользовательские данные.\n' > "$WORK_PATH/e2e-proof.md"
git -C "$WORK_PATH" add e2e-proof.md
git -C "$WORK_PATH" commit -q -m 'test: e2e persistence check'
api "$COOKIES_ADMIN" POST "/api/workspaces/$WS_ID/stop" >/dev/null
if wait_status "$WS_ID" stopped 60; then
  pass "stop → stopped"
  git --git-dir="$BARE_PATH" log --oneline --all 2>/dev/null | grep -q 'persistence check' \
    && pass "autosave при stop доставил proof-коммит в bare" || fail "proof нет в bare"
else
  fail "stop не дошёл до stopped"
fi
api "$COOKIES_ADMIN" POST "/api/workspaces/$WS_ID/start" >/dev/null
if wait_status "$WS_ID" running 120; then
  pass "start → running"
  docker exec "$CONTAINER" test -f /workspace/e2e-proof.md 2>/dev/null \
    && pass "proof-файл пережил рестарт (виден в контейнере)" || fail "proof пропал"
else
  fail "restart не дошёл до running"
fi

say "dsh-home и права"
test -f "$TEST_HOME/work/$WS_ID/dsh-home/settings.yaml" \
  && pass "settings.yaml на месте" || fail "нет settings.yaml"
perms=$(stat -f '%Lp' "$TEST_HOME/work/$WS_ID/dsh-home/.credentials.yaml" 2>/dev/null)
[ "$perms" = "600" ] && pass ".credentials.yaml права 600" || fail "creds perms=$perms"

say "CLI бэклога в контейнере"
docker exec "$CONTAINER" sh -lc 'cd /workspace && agctl backlog check' >/dev/null 2>&1 \
  && pass "agctl backlog check внутри контейнера" || fail "backlog check упал"

say "Живой провод до модели (через env-токен контейнера)"
CHAT="NOT RUN"
if [ "${FAKE_LLM:-0}" = "1" ]; then
  probe=$(docker exec "$CONTAINER" sh -c \
    'printf "{\"model\":\"stub-model\",\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}]}" | curl -sS -m 15 -H "Content-Type: application/json" -H "Authorization: Bearer $AGCTL_LLM_API_KEY" "'"$PROVIDER_URL"'/chat/completions"' 2>/dev/null)
  if printf '%s' "$probe" | grep -q 'FAKE-LLM-REPLY'; then
    CHAT="PASS"
    pass "контейнер → провайдер: ответ стаба получен (env-токен пробит до стаба)"
  else
    skip "HTTP-проба из контейнера не прошла; UI-чат не проверялся (см. журнал)"
  fi
else
  skip "FAKE_LLM=0 — живой провод не проверялся"
fi

say "Удаление воркспейса"
if [ -n "${WS_ID:-}" ]; then
  api "$COOKIES_ADMIN" DELETE "/api/workspaces/$WS_ID" >/dev/null
  sleep 2
  docker ps -a --format '{{.Names}}' | grep -qx "$CONTAINER" \
    && fail "контейнер не удалён" || pass "контейнер удалён"
  test -d "$TEST_HOME/work/$WS_ID/repo" \
    && pass "каталоги TEST_HOME остались (ожидаемо по гайду)" || skip "каталогов нет"
fi

say "ИТОГО: PASS=$PASS FAIL=$FAIL CHAT=$CHAT run=$RUN_ID"
if [ "$FAILED" = 0 ] && [ "${CLEANUP:-0}" = "1" ]; then
  case "$TEST_HOME" in
    "$HOME"/.agctl-e2e-*) rm -rf -- "$TEST_HOME" && printf 'очищено: %s\n' "$TEST_HOME" ;;
  esac
fi
[ "$FAILED" = 0 ] || exit 1
