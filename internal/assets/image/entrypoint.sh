#!/bin/sh
# agctl-entrypoint: готовит DSH_HOME (volume может быть пустым) и запускает
# dsh web. Апстрим dsh запрещает только wildcard-бинд, поэтому dsh слушает
# конкретный адрес eth0 контейнера (agctl-dsh-net print); наружу его отдаёт
# публикация порта Docker/k8s — релея в образе нет (экономит node-процесс
# и ~80 MB RSS на пользователя). Опционально поднимает sshd внутри контейнера,
# когда agctl передал публичный ключ.
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

# SSH-канал — включён только при наличии публичного ключа, выданного
# владельцем воркспейса через PATCH /api/workspaces/{id}/ssh. Порт контейнера
# 2222 публикуется Docker только на 127.0.0.1 хоста (не в LAN/Internet).
if [ -n "${AGCTL_SSH_PUBKEY:-}" ]; then
  mkdir -p /root/.ssh /run/sshd
  printf '%s\n' "$AGCTL_SSH_PUBKEY" > /root/.ssh/authorized_keys
  chmod 700 /root/.ssh
  chmod 600 /root/.ssh/authorized_keys
  ssh-keygen -A
  cat > /etc/ssh/sshd_config.d/agctl.conf <<'SSHCONF'
PermitRootLogin prohibit-password
PubkeyAuthentication yes
PasswordAuthentication no
KbdInteractiveAuthentication no
AuthorizedKeysFile .ssh/authorized_keys
AllowTcpForwarding no
X11Forwarding no
PermitTunnel no
AllowAgentForwarding no
SSHCONF
  /usr/sbin/sshd -t -p 2222
  /usr/sbin/sshd -D -e -p 2222 &
  echo "[agctl-entrypoint] SSH enabled on container port 2222 (key auth only)"
fi

BIND_IP="$(node /usr/local/bin/agctl-dsh-net print)"
echo "[agctl-entrypoint] dsh web на ${BIND_IP}:${DSH_PORT:-3080}"
exec dsh web --host "$BIND_IP" --port "${DSH_PORT:-3080}" --no-open "$@"
