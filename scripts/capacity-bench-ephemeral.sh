#!/bin/bash
# capacity-bench-ephemeral: цена «dsh headless на задачу» — эфемерная среда.
# Карточка agctl-hq capacity-ephemeral-bench; эпик night-dsh-plugins, волна 3.
# Методика как в capacity-bench.sh: изолированный DSH_HOME, стаб LLM,
# замеры real/RSS. Никакого agctl/Docker — измеряется сам движок.
#
#   scripts/capacity-bench-ephemeral.sh
#
# Серии:
#   warmup    — первый бут профиля + импорт settings.yaml (не считается)
#   seq x5    — 5 задач подряд: real_s, rss_mb каждой (медиана)
#   par x2/x4 — одновременные задачи: wall группы
# Выход: markdown-таблица в stdout + сырой jsonl в /tmp.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

RUN_ID="$(date +%Y%m%d-%H%M%S)"
BENCH_HOME="${BENCH_HOME:-/tmp/dsh-headless-bench-$RUN_ID}"
FAKE_PORT="${FAKE_PORT:-18767}"
PROFILE="bench-headless"
RAW="/tmp/agctl-ephemeral-$RUN_ID.jsonl"
TASK="Ответь ровно одним словом: ping"
FAKE_PID=""

umask 077
mkdir -p "$BENCH_HOME"
export DSH_HOME="$BENCH_HOME"
export AGCTL_LLM_API_KEY="sk-bench-local"

cleanup() {
  [ -n "$FAKE_PID" ] && kill "$FAKE_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

command -v dsh >/dev/null 2>&1 || { echo "нет dsh в PATH" >&2; exit 2; }

echo "bench-ephemeral $RUN_ID: home=$BENCH_HOME fake=:$FAKE_PORT" >&2

# 1) стаб LLM
python3 "$REPO_ROOT/scripts/fake-llm.py" --port "$FAKE_PORT" >"/tmp/agctl-eph-llm-$RUN_ID.log" 2>&1 &
FAKE_PID=$!

# 2) настройки провайдера (формат — как agctl RefreshProvider пишет в dsh-home)
cat > "$BENCH_HOME/settings.yaml" <<EOF
llm-pi-ai:
  providers:
    bench:
      apiKeyEnv: AGCTL_LLM_API_KEY
      api: openai-completions
      baseURL: http://127.0.0.1:$FAKE_PORT
      models:
        - id: stub-model
          name: stub-model
          contextWindow: 262144
          maxTokens: 65536
agent-default-model:
  provider: bench
  model: stub-model
EOF

run_task() { # $1 — метка серии; пишет jsonl-строку
  local label="$1" t0 t1
  t0=$(python3 -c 'import time; print(time.time())')
  /usr/bin/time -l dsh --profile "$PROFILE" "$TASK" \
    >"/tmp/bench-eph-$RUN_ID-last.out" 2>"/tmp/bench-eph-$RUN_ID-last.time"
  local rc=$?
  t1=$(python3 -c 'import time; print(time.time())')
  local rss
  rss=$(grep -o '[0-9]* *maximum resident set size' "/tmp/bench-eph-$RUN_ID-last.time" | grep -o '[0-9]*' | head -1)
  [ -z "$rss" ] && rss=0
  python3 - "$label" "$rc" "$t0" "$t1" "$rss" "$RAW" <<'EOF'
import json, sys
label, rc, t0, t1, rss, raw = sys.argv[1:7]
print(json.dumps({"series": label, "rc": int(rc), "wall_s": round(float(t1) - float(t0), 2),
                  "rss_bytes": int(rss)}), file=open(raw, "a"))
EOF
}

# 3) warmup: создаём кастомный профиль из shipped-шаблона headless и
#    убеждаемся, что провайдер bench подхватился из settings.yaml.
echo "warmup..." >&2
if [ ! -f "$BENCH_HOME/profiles/$PROFILE/package.json" ]; then
  dsh --profile "$PROFILE" --from-default-profile headless "init" \
    >"/tmp/bench-eph-$RUN_ID-init.out" 2>&1 || true
fi
run_task warmup
head -3 "/tmp/bench-eph-$RUN_ID-last.out" 2>/dev/null | sed 's/^/  out: /' >&2

# 4) последовательные 5
for i in 1 2 3 4 5; do run_task "seq"; done

# 5) параллельные группы
par_group() { # $1 — размер группы
  local n="$1" pids=() t0 t1
  t0=$(python3 -c 'import time; print(time.time())')
  for i in $(seq 1 "$n"); do
    ( /usr/bin/time -l dsh --profile "$PROFILE" "$TASK" \
        >"/dev/null" 2>"/tmp/bench-eph-$RUN_ID-par-$i.time" ) &
    pids+=($!)
  done
  for p in "${pids[@]}"; do wait "$p"; done
  t1=$(python3 -c 'import time; print(time.time())')
  local rss_sum=0
  for i in $(seq 1 "$n"); do
    r=$(grep -o '[0-9]* *maximum resident set size' "/tmp/bench-eph-$RUN_ID-par-$i.time" | grep -o '[0-9]*' | head -1)
    rss_sum=$((rss_sum + ${r:-0}))
  done
  python3 - "$n" "$t0" "$t1" "$rss_sum" "$RAW" <<'EOF'
import json, sys
n, t0, t1, rss, raw = sys.argv[1:6]
print(json.dumps({"series": f"par{n}", "rc": 0, "wall_s": round(float(t1) - float(t0), 2),
                  "rss_bytes": int(rss)}), file=open(raw, "a"))
EOF
}
par_group 2
par_group 4

# 6) сводка
python3 - "$RAW" <<'EOF'
import json, statistics, sys
rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
seq = [r for r in rows if r["series"] == "seq"]
print("| Серия | wall_s | RSS/задача, МБ |")
print("|---|---|---|")
if seq:
    walls = [r["wall_s"] for r in seq]
    rss = [r["rss_bytes"] / 1048576 for r in seq]
    print(f"| seq (медиана 5) | {statistics.median(walls):.1f} | {statistics.median(rss):.0f} |")
for n in (2, 4):
    par = [r for r in rows if r["series"] == f"par{n}"]
    if par:
        r = par[0]
        print(f"| par{n} (группа) | {r['wall_s']:.1f} | {r['rss_bytes'] / 1048576:.0f} суммарно |")
w = [r for r in rows if r["series"] == "warmup"]
if w:
    print(f"| warmup (не считается) | {w[0]['wall_s']:.1f} | {w[0]['rss_bytes'] / 1048576:.0f} |")
EOF
echo "raw: $RAW" >&2
