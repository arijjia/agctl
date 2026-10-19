#!/bin/bash
# night-profile: изолированный dsh web с плагином «Кабинет agctl».
# Эпик night-dsh-plugins, волна 1. Живая сессия владельца (дефолтный
# $DSH_HOME, порт 3080) не затрагивается: всё в отдельном DSH_HOME и на
# своём порту.
#
#   scripts/night-profile.sh                 # поднять/перезапустить
#   PORT=3556 scripts/night-profile.sh       # другой порт
#   scripts/night-profile.sh stop            # остановить
#
# После старта печатает URL (в т.ч. с boot-токеном из лога — по нему
# браузер логинится без ручного ввода) и делает smoke-проверку curl.
set -u
cd "$(dirname "$0")/.."
REPO_ROOT="$PWD"

PKG="$REPO_ROOT/dsh-plugins/cabinet"
DSH_HOME_TEST="${DSH_HOME_TEST:-/tmp/agctl-night-dsh-home}"
PORT="${PORT:-3555}"
PROFILE="night-cabinet"
PLUGIN_NAME="@arijjia/agctl-cabinet"
LOG="$DSH_HOME_TEST/web-$PORT.log"
PIDFILE="$DSH_HOME_TEST/web-$PORT.pid"

command -v dsh >/dev/null 2>&1 || { echo "night-profile: dsh не найден в PATH" >&2; exit 2; }
command -v node >/dev/null 2>&1 || { echo "night-profile: node не найден" >&2; exit 2; }
[ -f "$PKG/lib/client.js" ] || { echo "night-profile: нет $PKG/lib/client.js" >&2; exit 2; }
[ -f "$PKG/lib/index.js" ] || { echo "night-profile: нет $PKG/lib/index.js" >&2; exit 2; }

mkdir -p "$DSH_HOME_TEST"

stop_old() {
  if [ -f "$PIDFILE" ]; then
    old="$(cat "$PIDFILE" 2>/dev/null || true)"
    [ -n "${old:-}" ] && kill "$old" >/dev/null 2>&1 || true
    rm -f "$PIDFILE"
    sleep 1
  fi
}

if [ "${1:-}" = "stop" ]; then
  stop_old
  echo "night-profile: остановлен (порт $PORT)"
  exit 0
fi

stop_old
rm -f "$LOG"

export DSH_HOME="$DSH_HOME_TEST"

# 1) База профиля: наш кастомный профиль должен нести тот же стек бандлов,
#    что и рабочий web-профиль (dsh-base + dsh-web-app), иначе web-композиция
#    не соберётся. Если web-профиля ещё нет в тестовом home — бутнем web
#    одноразово на свободном порту (--port 0), чтобы шаблон материализовался.
if [ ! -f "$DSH_HOME_TEST/profiles/web/package.json" ]; then
  echo "night-profile: инициализирую web-профиль (одноразовый бут, порт 0)"
  tmplog="$DSH_HOME_TEST/web-init.log"
  dsh web --port 0 --no-open > "$tmplog" 2>&1 &
  initpid=$!
  for i in $(seq 1 20); do
    sleep 1
    grep -q 'dsh web: http' "$tmplog" 2>/dev/null && break
    kill -0 "$initpid" 2>/dev/null || break
  done
  kill "$initpid" >/dev/null 2>&1 || true
  wait "$initpid" 2>/dev/null || true
  [ -f "$DSH_HOME_TEST/profiles/web/package.json" ] || {
    echo "night-profile: web-профиль не материализовался, хвост:" >&2
    tail -10 "$tmplog" >&2
    exit 3
  }
fi

# 2) Ставим наш плагин в профиль night-cabinet (инициализирует профиль).
dsh plugin --profile "$PROFILE" add "file:$PKG" >/dev/null || {
  echo "night-profile: plugin add не удался" >&2
  exit 3
}

# 3) Включаем плагин в bundles профиля: база — бандлы web-профиля, наш —
#    последним слоем (порядок dsh.profile.bundles имеет значение).
node -e '
const fs = require("fs");
const [profileDir, pluginName] = process.argv.slice(1);
const p = profileDir + "/package.json";
const j = JSON.parse(fs.readFileSync(p, "utf8"));
j.dsh = j.dsh || {};
j.dsh.profile = j.dsh.profile || {};
const web = JSON.parse(fs.readFileSync(profileDir + "/../web/package.json", "utf8"));
const base = ((web.dsh || {}).profile || {}).bundles || ["@deepseek-ai/dsh-base", "@deepseek-ai/dsh-web-app"];
const bundles = base.filter((x) => x !== pluginName).concat(pluginName);
j.dsh.profile.bundles = bundles;
fs.writeFileSync(p, JSON.stringify(j, null, 2) + "\n");
console.log("night-profile: bundles =", JSON.stringify(bundles));
' "$DSH_HOME_TEST/profiles/$PROFILE" "$PLUGIN_NAME"

# 4) Бут изолированного web на своём порту (приложение определяется стеком
#    бандлов профиля — dsh-web-app; --port/--no-open уходят в web-апп).
nohup dsh --profile "$PROFILE" --port "$PORT" --no-open > "$LOG" 2>&1 &
child=$!
echo "$child" > "$PIDFILE"

# 5) Smoke: ждём слушателя до ~20с.
ok=""
for i in $(seq 1 20); do
  sleep 1
  code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "http://127.0.0.1:$PORT/" || true)"
  if [ "${code:-}" != "000" ]; then ok="$code"; break; fi
  kill -0 "$child" 2>/dev/null || break
done

if [ -z "$ok" ]; then
  echo "night-profile: инстанс не поднялся за 20с, хвост лога:" >&2
  tail -20 "$LOG" >&2
  exit 4
fi

echo "night-profile: инстанс жив на http://127.0.0.1:$PORT/ (HTTP $ok)"
echo "night-profile: лог: $LOG"
token="$(grep -o 'token=[A-Za-z0-9_-]*' "$LOG" | head -1 | cut -d= -f2 || true)"
if [ -n "$token" ]; then
  echo "night-profile: URL с boot-токеном (автовход): http://127.0.0.1:$PORT/?token=$token"
else
  echo "night-profile: boot-токен в логе не найден — смотри $LOG"
fi
