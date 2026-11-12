# agctl-dsh: рабочее место агента (dsh web на eth0 + agctl-бинарь).
# Слои: base (node+тулчейн) → dsh (npm, pinned) → MCP-прогрев → runtime.
# DSH_HOME — ТОЛЬКО volume в рантайме (сессии/креды никогда не в слоях).
# Секреты (LLM-токен) — только env/файл в volume, никогда в образе.
# Linux-сборка agctl кладётся в контекст сборки файлом ./agctl (make image).
FROM node:22-bookworm-slim AS base
RUN apt-get update \
  && apt-get install -y --no-install-recommends python3 python3-pytest python3-pip make g++ ca-certificates curl git openssh-server iptables \
  && rm -rf /var/lib/apt/lists/* \
  && pip3 install --no-cache-dir --break-system-packages python-pptx \
  && npm install -g npm@12 --no-audit --no-fund \
  && npm --version
# iptables — egress-allowlist: хелпер применения DOCKER-USER правил (egress.go);
# git нужен агенту в воркспейсе; npm>=11 ради allow-scripts (нативные депы dsh);
# python3+pytest — тулзы доменных шаблонов (например 1c-vibe);
# python-pptx — навык презентаций по шаблону вуза (каталог возможностей).

FROM base AS dsh
# идентичность git для агента (в любом репо контейнера)
RUN git config --system user.name "agent" \
  && git config --system user.email "agent@workspace.local" \
  && git config --system init.defaultBranch main \
  && git config --system --add safe.directory '*' \
  && npm config set allow-scripts \
      @deepseek-ai/dsh-subprocess-local,koffi,node-pty,fsevents,protobufjs,esbuild \
      --location=user \
  && npm install -g --no-audit --no-fund @deepseek-ai/dsh@0.2.1-rc.5 \
  && dsh --version
ENV PATH="/opt/dsh/bin:${PATH}"

FROM dsh AS mcp-warm
# Прогрев npx-кэша MCP web-профиля (паттерн kb/dsh/mgmt: timeout 124 = успех).
ENV NPM_MCP_CACHE=/opt/npm-mcp
RUN mkdir -p "$NPM_MCP_CACHE" \
  && for pkg in duckduckgo-mcp-server @upstash/context7-mcp; do \
       echo "prewarm: $pkg"; \
       timeout 25 npx --yes --cache "$NPM_MCP_CACHE" "$pkg" </dev/null >/dev/null 2>&1; \
       code=$?; [ "$code" -eq 124 ] || [ "$code" -eq 0 ] || { echo "prewarm FAIL $pkg: $code"; exit 1; }; \
     done \
  && du -sh "$NPM_MCP_CACHE"

FROM dsh AS runtime
COPY --from=mcp-warm /opt/npm-mcp /opt/npm-mcp
# Office-превью в контейнере: кит @deepseek-ai/libreoffice-kit (движок превью
# dsh) не поставляет Linux-сборки (карточка office-preview-in-container), поэтому
# конвертацию в PDF делает системный LibreOffice — только impress (pptx-кейс,
# бюджет образа ~300-400 МиБ; writer/calc для docx/xlsx — отдельное решение).
# Слой в runtime, а не в base: apt-изменение не инвалидирует npm-слои dsh/mcp.
RUN apt-get update \
  && apt-get install -y --no-install-recommends libreoffice-impress \
  && rm -rf /var/lib/apt/lists/*
ENV NPM_MCP_CACHE=/opt/npm-mcp
ENV DSH_HOME=/home/dsh
# Вариант образа «без SSH» (лаунчер developer'а): собирается с
# --build-arg AGCTL_NO_SSHD=1 (тег agctl-dsh-nossh:*) — entrypoint не поднимает
# sshd даже при переданном публичном ключе.
ARG AGCTL_NO_SSHD=0
ENV AGCTL_NO_SSHD=${AGCTL_NO_SSHD}
COPY agctl /usr/local/bin/agctl
# Скелет DSH_HOME: entrypoint сеет его в пустой том (см. /opt/dsh-skel).
COPY skel/ /opt/dsh-skel/
COPY entrypoint.sh /usr/local/bin/agctl-entrypoint
COPY dsh-net.mjs /usr/local/bin/agctl-dsh-net
COPY office2pdf /usr/local/bin/office2pdf
RUN chmod +x /usr/local/bin/agctl /usr/local/bin/agctl-entrypoint /usr/local/bin/agctl-dsh-net /usr/local/bin/office2pdf && mkdir -p /home/dsh /workspace
WORKDIR /workspace
EXPOSE 3080 2222
# Живость dsh: любой HTTP-ответ (в т.ч. 401/303 без cookie) = процесс жив и
# слушает dsh web на eth0-адресе контейнера (agctl-dsh-net health); обрыв/
# таймаут/5xx = unhealthy. Start-period — с запасом на первый прогрев
# (MCP-кэш уже вшит). Статус читает agctl (reconcile).
HEALTHCHECK --interval=30s --timeout=6s --start-period=150s --retries=3 \
  CMD node /usr/local/bin/agctl-dsh-net health
ENTRYPOINT ["agctl-entrypoint"]
