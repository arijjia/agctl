#!/bin/bash
# e2e-smoke (v3): изолированный дымовой прогон agctl под модель v3 —
# персональный контейнер + проекты. Всё в своём TEST_HOME, свой тег образа,
# свой порт; секреты/токены не печатаются.
#
#   TEST_IMAGE=agctl-dsh:0.2.0-rc2-e2e-n1 bin/e2e-smoke.sh
#
# Сценарий v3 (v2-воркспейсы удалены, POST /api/workspaces → 410):
#   регистрация/роли → tombstone v2 → проект (создание/листинг/git-репо) →
#   личный чат /chat/ (подъём контейнера, шлюз, asset) → docker-инспекты →
#   сон/пробуждение контейнера с сохранением тома → autosave проекта →
#   dsh-home/креды → живой провод до стаба LLM.
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
JAR="$TEST_HOME/qa-admin-dsh.cookies"
FAKE_PORT="${FAKE_PORT:-18765}"
FAKE_PID=""; SERVE_PID=""
PROVIDER_URL="http://provider.invalid/v1"   # недоступный — провод будет NOT RUN
LLM_KEY="sk-e2e-fake-not-a-secret"
LAST="/tmp/agctl-e2e-$RUN_ID-last.json"
CHAT_HTML="$TEST_HOME/chat.html"

umask 077
mkdir -p "$TEST_HOME"
say "Прогон $RUN_ID: home=$TEST_HOME addr=$TEST_ADDR image=$TEST_IMAGE"

preflight() {
  command -v go >/dev/null || { echo "нет go" >&2; exit 2; }
  command -v docker >/dev/null || { echo "нет docker" >&2; exit 2; }
  docker version --format '{{.Server.Version}}' >/dev/null || { echo "docker мёртв" >&2; exit 2; }
  docker image inspect "$TEST_IMAGE" >/dev/null 2>&1 || { echo "образ $TEST_IMAGE не собран" >&2; exit 2; }
  if nc -z 127.0.0.1 "$TEST_PORT" 2>/dev/null; then
    echo "порт $TEST_PORT занят — выбери другой TEST_ADDR" >&2; exit 2
  fi
  if [ ! -x "$AGCTL_BIN" ]; then
    say "Сборка бинаря"
    make web_build >/dev/null 2>&1 || { echo "web_build упал" >&2; exit 2; }
    go build -trimpath -o "$AGCTL_BIN" ./cmd/agctl || { echo "go build упал" >&2; exit 2; }
  fi
  pass "preflight: инструменты на месте, порт $TEST_PORT свободен, образ есть"
}

start_fake_llm() {
  [ "${FAKE_LLM:-0}" = "1" ] || return 0
  FAKE_BIND="${FAKE_LLM_BIND:-127.0.0.1}"
  python3 "$REPO_ROOT/bin/fake-llm.py" --port "$FAKE_PORT" --bind "$FAKE_BIND" \
    >"/tmp/agctl-fake-llm-$RUN_ID.log" 2>&1 &
  FAKE_PID=$!
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    curl -s -o /dev/null "http://127.0.0.1:$FAKE_PORT/v1/models" && break
    sleep 0.3
  done
  PROVIDER_URL="http://host.docker.internal:$FAKE_PORT/v1"
  pass "fake-llm: стаб поднят на $FAKE_BIND:$FAKE_PORT (модели: stub-model)"
}

api() { # api <cookies|-> <method> <path> [json]
  local c="$1" m="$2" p="$3" d="${4:-}"
  local args=(-sS -o "$LAST" -w '%{http_code}' -X "$m")
  [ "$c" != "-" ] && args+=(-b "$c")
  [ -n "$d" ] && args+=(-H 'Content-Type: application/json' -d "$d")
  curl "${args[@]}" "$BASE_URL$p"
}

jsonget() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$1"')' "$LAST" 2>/dev/null; }

# wait_chat_ui: /chat/ отвечает DSH UI (не страницей прогрева) — признак:
# бандл src="./assets/*.js". Контейнер поднимается до ~минуты.
wait_chat_ui() { # <cookies> <timeout_s>
  local c="$1" t="$2" waited=0
  while [ "$waited" -lt "$t" ]; do
    code=$(curl -sS -c "$JAR" -b "$c" -L -o "$CHAT_HTML" -w '%{http_code}' "$BASE_URL/chat/")
    if [ "$code" = 200 ] && grep -q 'src="./assets/' "$CHAT_HTML" 2>/dev/null; then
      return 0
    fi
    sleep 3; waited=$((waited + 3))
  done
  return 1
}

# chat_container: имя живого контейнера личного чата админа (u1) через
# админ-API — без угадывания хэша home в имени agctl-chat-<hash>-u1.
chat_container() {
  api "$COOKIES_ADMIN" GET /api/admin/containers >/dev/null
  python3 - "$LAST" <<'EOF'
import json, sys
rows = json.load(open(sys.argv[1])).get("containers", [])
for c in rows:
    if c.get("userId") == 1 and c.get("state") == "running":
        print(c["name"]); break
EOF
}

cleanup() {
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" 2>/dev/null
}
trap cleanup EXIT INT TERM

preflight
start_fake_llm

say "Сервис agctl"
AGCTL_LLM_API_KEY="$LLM_KEY" "$AGCTL_BIN" serve --home "$TEST_HOME" --addr "$TEST_ADDR" \
  --image "$TEST_IMAGE" --mem-gb 2 --cpus 2 \
  --llm-base-url "$PROVIDER_URL" --llm-models stub-model \
  >"/tmp/agctl-serve-$RUN_ID.log" 2>&1 &
SERVE_PID=$!
BASE_URL="http://$TEST_ADDR"
up=0
for _ in $(seq 1 40); do
  [ "$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/" 2>/dev/null)" = "200" ] && { up=1; break; }
  sleep 0.5
done
if [ "$up" != 1 ]; then
  # Без работающего UI остальное бессмысленно: вместо пяти минут красных
  # FAIL — сразу причина (хвост журнала serve; чаще всего CLI-флаг, которого нет).
  fail "UI не поднялся — хвост журнала serve:"
  tail -n 30 "/tmp/agctl-serve-$RUN_ID.log" | sed 's/^/  | /'
  exit 1
fi
pass "UI отвечает 200 на $BASE_URL"

say "Регистрация и роли"
code=$(curl -sS -c "$COOKIES_ADMIN" -o "$LAST" -w '%{http_code}' -H 'Content-Type: application/json' \
  -d '{"login":"qa-admin","password":"TempTest-123"}' "$BASE_URL/api/register")
[ "$code" = 200 ] && pass "регистрация qa-admin: $code" || fail "register admin: $code"
code=$(curl -sS -c "$COOKIES_USER" -o "$LAST" -w '%{http_code}' -H 'Content-Type: application/json' \
  -d '{"login":"qa-user","password":"TempTest-456"}' "$BASE_URL/api/register")
[ "$code" = 200 ] && pass "регистрация qa-user: $code" || fail "register user: $code"
role=$(curl -sS -b "$COOKIES_ADMIN" "$BASE_URL/api/me" | python3 -c 'import json,sys; print(json.load(sys.stdin)["user"]["role"])' 2>/dev/null)
[ "$role" = admin ] && pass "первый пользователь — admin" || fail "роль: $role"
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/api/projects")
[ "$code" = 401 ] && pass "без входа /api/projects → 401" || fail "аноним: $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "$BASE_URL/api/admin/users")
[ "$code" = 403 ] && pass "не-admin /api/admin/users → 403" || fail "users: $code"
code=$(curl -sS -o /dev/null -w '%{http_code}' -b "$COOKIES_USER" "$BASE_URL/api/admin/audit")
[ "$code" = 403 ] && pass "не-admin /api/admin/audit → 403" || fail "audit: $code"

say "Tombstone v2: воркспейсы удалены в v3"
code=$(api "$COOKIES_ADMIN" POST /api/workspaces '{"name":"legacy"}')
[ "$code" = 410 ] && pass "POST /api/workspaces → 410 (v2 удалены)" || fail "workspaces: $code"

say "Проекты: создание, листинг, git-репозиторий"
code=$(api "$COOKIES_ADMIN" POST /api/projects '{"name":"e2e-proj"}')
[ "$code" = 201 ] && pass "создание проекта: 201" || { fail "create project: $code"; cat "$LAST" 2>/dev/null; }
api "$COOKIES_ADMIN" GET /api/projects >/dev/null
names=$(jsonget "['projects']" 2>/dev/null | grep -c "'name': 'e2e-proj'")
[ "$names" -ge 1 ] && pass "проект в списке" || fail "проекта нет в списке"
code=$(api "$COOKIES_USER" GET /api/projects/e2e-proj/files)
case "$code" in 403|404) pass "чужой проект по files API → $code" ;; *) fail "изоляция проектов: $code" ;; esac
PROJ_DIR="$TEST_HOME/chats/u1/work/e2e-proj"
if [ -d "$PROJ_DIR/.git" ]; then
  pass "каталог проекта с git-репо: chats/u1/work/e2e-proj"
  git -C "$PROJ_DIR" log --oneline 2>/dev/null | grep -q . \
    && pass "git-история проекта непуста" || fail "git пуст"
  grep -q '.env' "$PROJ_DIR/.gitignore" 2>/dev/null \
    && pass ".gitexclude секретов на месте" || fail ".gitignore без секретных паттернов"
else
  fail "нет $PROJ_DIR/.git"
fi

say "Личный чат: вход через шлюз, подъём персонального контейнера"
if wait_chat_ui "$COOKIES_ADMIN" 240; then
  size=$(wc -c < "$CHAT_HTML" | tr -d ' ')
  pass "DSH UI через /chat/: 200 (${size}b), не страница прогрева"
  asset=$(python3 -c 'import re,sys; s=open(sys.argv[1]).read(); m=re.search(r"src=\"\./(assets/[^\"]+\.js)\"", s); print(m.group(1) if m else "")' "$CHAT_HTML")
  if [ -n "$asset" ]; then
    code=$(curl -sS -b "$JAR" -o /dev/null -w '%{http_code}' "$BASE_URL/chat/$asset")
    [ "$code" = 200 ] && pass "DSH JS asset через шлюз: $code" || fail "asset: $code"
  else
    fail "не удалось извлечь asset URL из DSH HTML"
  fi
else
  fail "/chat/ не отдал DSH UI за 240с"
  tail -n 15 "/tmp/agctl-serve-$RUN_ID.log" | sed 's/^/  | /'
fi
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/chat/")
[ "$code" = 401 ] && pass "аноним /chat/ → 401" || fail "аноним /chat/: $code"

CONTAINER="$(chat_container)"
if [ -n "$CONTAINER" ]; then
  pass "персональный контейнер поднят: $CONTAINER"
else
  fail "контейнер чата не найден через /api/admin/containers"
fi

say "Docker: порты, лимиты, монтирования"
if [ -n "${CONTAINER:-}" ]; then
  docker inspect --format '{{range $p, $b := .NetworkSettings.Ports}}{{$p}} -> {{range $b}}{{.HostIP}}:{{.HostPort}} {{end}}{{end}}' "$CONTAINER" 2>/dev/null | grep -q '127.0.0.1:' \
    && pass "порт контейнера опубликован на 127.0.0.1" || fail "порт не на loopback"
  mem=$(docker inspect --format '{{.HostConfig.Memory}}' "$CONTAINER" 2>/dev/null)
  [ "$mem" = 2147483648 ] && pass "лимит памяти 2GiB" || fail "memory=$mem"
  mounts=$(docker inspect --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}; {{end}}' "$CONTAINER" 2>/dev/null)
  echo "$mounts" | grep -q '/workspace' && echo "$mounts" | grep -q '/home/dsh' \
    && pass "монтирования /workspace и /home/dsh" || fail "mounts: $mounts"
  srcok=$(printf '%s' "$mounts" | grep -c "$TEST_HOME/chats/u1")
  [ "$srcok" -ge 1 ] && pass "тома живут в TEST_HOME (изоляция прогона)" || fail "чужие источники монтирований"
else
  skip "инспекты Docker: контейнера нет"
fi

say "Сон и пробуждение контейнера (том переживает)"
PERSIST_OK=0
if [ -n "${CONTAINER:-}" ]; then
  printf 'e2e persistence proof; не пользовательские данные\n' > "$PROJ_DIR/e2e-proof.md"
  code=$(api "$COOKIES_ADMIN" POST "/api/admin/containers/$CONTAINER/stop")
  [ "$code" = 200 ] && pass "admin stop → 200" || fail "stop: $code"
  slept=0
  while [ "$slept" -lt 30 ]; do
    state=$(docker inspect --format '{{.State.Status}}' "$CONTAINER" 2>/dev/null || echo gone)
    [ "$state" = gone ] && break
    sleep 1; slept=$((slept + 1))
  done
  [ "$state" = gone ] && pass "контейнер удалён (compute), том сохранён" || fail "контейнер не удалён: $state"
  if wait_chat_ui "$COOKIES_ADMIN" 240; then
    NEW="$(chat_container)"
    [ -n "$NEW" ] && pass "повторный вход пересоздал контейнер: $NEW" || fail "контейнер не пересоздан"
    docker exec "$NEW" test -f /workspace/e2e-proj/e2e-proof.md 2>/dev/null \
      && { pass "файл проекта пережил сон/пробуждение (виден в контейнере)"; PERSIST_OK=1; } \
      || fail "proof-файл не виден в контейнере"
    CONTAINER="$NEW"
  else
    fail "после сна /chat/ не поднялся за 240с"
  fi
else
  skip "сон/пробуждение: контейнера нет"
fi

say "Autosave проекта (локальный commit без push)"
if [ "${PERSIST_OK}" = 1 ]; then
  # Тикер autosave — каждые 2 минуты; ждём появления локального коммита.
  asok=0; waited=0
  while [ "$waited" -lt 180 ]; do
    if git -C "$PROJ_DIR" log --oneline 2>/dev/null | grep -q 'autosave'; then asok=1; break; fi
    sleep 5; waited=$((waited + 5))
  done
  if [ "$asok" = 1 ]; then
    pass "autosave закоммитил изменения локально"
    git -C "$PROJ_DIR" remote get-url origin >/dev/null 2>&1 \
      && fail "у проекта появился remote (не должно быть)" \
      || pass "проект без remote: push невозможен"
  else
    fail "autosave не закоммитил за 180с"
  fi
else
  skip "autosave: нет живого контейнера после пробуждения"
fi

say "dsh-home и права"
CRED="$TEST_HOME/chats/u1/dsh-home/.credentials.yaml"
if [ -f "$CRED" ]; then
  grep -q 'api_key' "$CRED" \
    && pass "креды провайдера импортированы в dsh-home" \
    || fail "credentials без ключа провайдера"
else
  fail "нет .credentials.yaml в dsh-home"
fi
perms=$(stat -c '%a' "$CRED" 2>/dev/null || stat -f '%Lp' "$CRED" 2>/dev/null)
[ "$perms" = 600 ] && pass ".credentials.yaml права 600" || fail "creds perms=$perms"

say "Живой провод до модели (через env-токен контейнера)"
CHAT="NOT RUN"
if [ "${FAKE_LLM:-0}" = "1" ] && [ -n "${CONTAINER:-}" ]; then
  probe=$(docker exec "$CONTAINER" sh -c \
    'printf "{\"model\":\"stub-model\",\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}]}" | curl -sS -m 15 --data-binary @- -H "Content-Type: application/json" -H "Authorization: Bearer $AGCTL_LLM_API_KEY" "'"$PROVIDER_URL"'/chat/completions"' 2>&1)
  if printf '%s' "$probe" | grep -q 'FAKE-LLM-REPLY'; then
    CHAT="PASS"
    pass "контейнер → провайдер: ответ стаба получен (env-токен пробит до стаба)"
  else
    printf '  probe head: %.200s\n' "$probe"
    skip "HTTP-проба из контейнера не прошла; UI-чат не проверялся (см. журнал)"
  fi
else
  skip "FAKE_LLM=0 — живой провод не проверялся"
fi

say "Удаление проекта"
code=$(api "$COOKIES_ADMIN" DELETE /api/projects/e2e-proj)
[ "$code" = 200 ] && pass "DELETE проекта: 200" || fail "delete project: $code"
api "$COOKIES_ADMIN" GET /api/projects >/dev/null
names=$(jsonget "['projects']" 2>/dev/null | grep -c "'name': 'e2e-proj'")
[ "$names" = 0 ] && pass "проект исчез из списка" || fail "проект остался в списке"

say "ИТОГО: PASS=$PASS FAIL=$FAIL CHAT=$CHAT run=$RUN_ID"
if [ "$FAILED" = 0 ] && [ "${CLEANUP:-0}" = "1" ]; then
  case "$TEST_HOME" in
    "$HOME"/.agctl-e2e-*) rm -rf -- "$TEST_HOME" && printf 'очищено: %s\n' "$TEST_HOME" ;;
  esac
fi
[ "$FAILED" = 0 ] || exit 1
