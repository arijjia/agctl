#!/bin/sh
# agctl-entrypoint: готовит DSH_HOME (volume может быть пустым) и запускает
# dsh web. dsh слушает только loopback (апстрим запрещает --host 0.0.0.0),
# поэтому наружу его отдаёт L4-релей 0.0.0.0:3080 -> 127.0.0.1:3099.
set -e
SKEL=/opt/dsh-skel

if [ ! -f "$DSH_HOME/profiles/web/package.json" ]; then
  echo "[agctl-entrypoint] сею скелет DSH_HOME -> $DSH_HOME"
  mkdir -p "$DSH_HOME"
  cp -R "$SKEL/." "$DSH_HOME/"
fi

# Страховка: live-patchReload требует --expose-internals (HMR-крах) — чиним.
PJ="$DSH_HOME/profiles/web/package.json"
if [ -f "$PJ" ] && grep -q '"patchReload": *"live"' "$PJ"; then
  echo "[agctl-entrypoint] patchReload live -> startup ($PJ)"
  node -e "const fs=require('fs');const p=process.argv[1];const d=JSON.parse(fs.readFileSync(p,'utf8'));d.dsh.profile.patchReload='startup';fs.writeFileSync(p,JSON.stringify(d,null,2));" "$PJ"
fi

RELAY_PORT="${RELAY_PORT:-3080}" DSH_PORT="${DSH_PORT:-3099}" node /usr/local/bin/agctl-relay &
exec dsh web --port "${DSH_PORT:-3099}" --no-open "$@"
