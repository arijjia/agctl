#!/bin/bash
# capacity-bench: замер ёмкости agctl для карточки runtime-mlops-capacity.
# Изолированный TEST_HOME, уникальный порт, стаб LLM. На выходе — таблица
# markdown с числами и сырой JSON в /tmp.
#
#   TEST_IMAGE=agctl-dsh:0.2.0-rc2-e2e-n1 scripts/capacity-bench.sh [N]
#
# Замеряет на каждом воркспейсе:
#   cold_start_s  — create → running (первый бут: provision + контейнер)
#   warm_start_s  — stop → start → running
#   idle_ram_mb / idle_cpu_pct — docker stats после 20с покоя
#   load_ram_mb / load_cpu_pct — под chat-нагрузкой (6 проб к провайдеру)
# Плюс RSS agctl-сервера на каждой точке N.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

N_MAX="${1:-4}"
[ -n "${TEST_IMAGE:-}" ] || { echo "TEST_IMAGE не задан" >&2; exit 2; }
TEST_ADDR="${TEST_ADDR:-127.0.0.1:18929}"
TEST_PORT="${TEST_ADDR##*:}"
AGCTL_BIN="${AGCTL_BIN:-$REPO_ROOT/dist/agctl}"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
TEST_HOME="$HOME/.agctl-cap-$RUN_ID"
COOKIES="$TEST_HOME/qa.cookies"
FAKE_PORT="${FAKE_PORT:-18766}"
RAW="/tmp/agctl-capacity-$RUN_ID.jsonl"
TABLE="/tmp/agctl-capacity-$RUN_ID.md"
FAKE_PID=""; SERVE_PID=""

umask 077
mkdir -p "$TEST_HOME"
echo "capacity-bench $RUN_ID: home=$TEST_HOME image=$TEST_IMAGE N=$N_MAX" >&2

cleanup() {
  [ -n "$SERVE_PID" ] && kill "$SERVE_PID" 2>/dev/null
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" 2>/dev/null
  # остановить контейнеры бенча (только свои, по лейблу+run_id в имени)
  docker ps -q --filter label=agctl.workspace | while read -r cid; do
    name=$(docker inspect --format '{{.Name}}' "$cid" 2>/dev/null | sed 's|/||')
    case "$name" in agctl-w-*) docker rm -f "$cid" >/dev/null 2>&1 ;; esac
  done
}
trap cleanup EXIT INT TERM

# preflight
command -v docker >/dev/null || { echo "нет docker" >&2; exit 2; }
docker version --format '{{.Server.Version}}' >/dev/null || exit 2
if nc -z 127.0.0.1 "$TEST_PORT" 2>/dev/null; then
  echo "порт $TEST_PORT занят — выбери другой TEST_ADDR" >&2; exit 2
fi
[ -x "$AGCTL_BIN" ] || { echo "нет бинаря $AGCTL_BIN" >&2; exit 2; }

python3 scripts/fake-llm.py --port "$FAKE_PORT" >"/tmp/agctl-cap-llm-$RUN_ID.log" 2>&1 &
FAKE_PID=$!
sleep 1

"$AGCTL_BIN" serve --home "$TEST_HOME" --addr "$TEST_ADDR" \
  --image "$TEST_IMAGE" --max-per-user 16 --mem-gb 2 --cpus 2 \
  >"/tmp/agctl-cap-serve-$RUN_ID.log" 2>&1 &
SERVE_PID=$!
BASE_URL="http://$TEST_ADDR"
for _ in $(seq 1 40); do
  [ "$(curl -sS -o /dev/null -w '%{http_code}' "$BASE_URL/" 2>/dev/null)" = "200" ] && break
  sleep 0.5
done

curl -sS -c "$COOKIES" -o /dev/null -H 'Content-Type: application/json' \
  -d '{"login":"bench","password":"Bench-123456"}' "$BASE_URL/api/register"

jsonget() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$1"')' "$LAST" 2>/dev/null; }
LAST="/tmp/agctl-cap-last-$RUN_ID.json"
api() { # api <method> <path> [json] — cookies фиксированы на бенч-пользователя
  local m="$1" p="$2" d="${3:-}"
  curl -sS -o "$LAST" -w '%{http_code}' -b "$COOKIES" -H 'Content-Type: application/json' \
    ${d:+-d "$d"} -X "$m" "$BASE_URL$p"
}

serve_rss_mb() {
  ps -o rss= -p "$SERVE_PID" 2>/dev/null | awk '{printf "%.0f", $1/1024}'
}

ws_ram_cpu() { # <wsID> <mode-label> — docker stats одним сэмплом (no-stream)
  local cid
  cid=$(docker ps -q --filter "label=agctl.workspace=$1")
  [ -n "$cid" ] || { echo "0 0"; return; }
  docker stats --no-stream --format '{{.MemUsage}} {{.CPUPerc}}' "$cid" 2>/dev/null | awk '{
    ram=$1; unit=$2; cpu=$3;
    sub(/[A-Za-z]+/, "", unit);
    mult = (index($2, "GiB") > 0) ? 1024 : 1;
    printf "%.0f %.2f", ram*mult, cpu
  }'
}

chat_load() { # <wsID> — 6 последовательных проб к провайдеру через env-токен
  local cid="agctl-$1"
  for _ in 1 2 3 4 5 6; do
    docker exec "$cid" sh -c 'printf "{\"model\":\"stub-model\",\"messages\":[{\"role\":\"user\",\"content\":\"load\"}]}" | curl -sS -m 10 --data-binary @- -H "Content-Type: application/json" -H "Authorization: Bearer $AGCTL_LLM_API_KEY" "http://host.docker.internal:'"$FAKE_PORT"'/v1/chat/completions"' >/dev/null 2>&1
  done
}

wait_status() { # <id> <status> <timeout_s> — печатает прошедшие секунды
  local id="$1" want="$2" t="$3" waited=0
  while [ "$waited" -lt "$t" ]; do
    api GET "/api/workspaces/$id" >/dev/null
    [ "$(jsonget "['workspace']['status']")" = "$want" ] && { echo "$waited"; return 0; }
    sleep 1; waited=$((waited + 1))
  done
  echo ""
  return 1
}

printf '| N мест | cold_start_s | warm_start_s | idle_ram_mb | idle_cpu_%% | load_ram_mb | load_cpu_%% | agctl_rss_mb |\n' > "$TABLE"
printf '|---|---|---|---|---|---|---|---|\n' >> "$TABLE"

declare -a WS_IDS=()
for i in $(seq 1 "$N_MAX"); do
  code=$(api POST /api/workspaces "{\"name\":\"cap-$i\",\"kit\":\"base\",\"components\":[\"core-workspace\",\"core-instructions\"],\"baseURL\":\"http://host.docker.internal:$FAKE_PORT/v1\",\"models\":[\"stub-model\"],\"apiKey\":\"sk-cap-fake\"}")
  [ "$code" = 202 ] || { echo "create cap-$i: $code" >&2; continue; }
  id=$(jsonget "['workspace']['id']")
  WS_IDS+=("$id")

  cold=$(wait_status "$id" running 240)
  [ -z "$cold" ] && { echo "cap-$i не поднялся" >&2; continue; }

  # idle: даём 20с покоя после бута
  sleep 20
  idle=$(ws_ram_cpu "$id")

  # warm restart
  api POST "/api/workspaces/$id/stop" >/dev/null
  warm=$(wait_status "$id" stopped 60)
  api POST "/api/workspaces/$id/start" >/dev/null
  warm2=$(wait_status "$id" running 120)
  warm_total=$(( ${warm:-0} + ${warm2:-0} ))

  # load: chat-пробы, сэмплим stats во время
  chat_load "$id" &
  load_pid=$!
  sleep 2
  load=$(ws_ram_cpu "$id")
  wait $load_pid 2>/dev/null

  rss=$(serve_rss_mb)
  line="| $i | ${cold:-?} | ${warm_total:-?} | $(echo $idle | awk '{print $1}') | $(echo $idle | awk '{print $2}') | $(echo $load | awk '{print $1}') | $(echo $load | awk '{print $2}') | $rss |"
  echo "$line" | tee -a "$TABLE"
  printf '{"n":%s,"cold":%s,"warm":%s,"idle":"%s","load":"%s","agctl_rss_mb":%s}\n' \
    "$i" "$cold" "$warm_total" "$idle" "$load" "$rss" >> "$RAW"
done

echo "" >&2
echo "=== Таблица: $TABLE ===" >&2
cat "$TABLE" >&2
echo "=== Сырые данные: $RAW ===" >&2
