# agctl-dsh: рабочее место агента (dsh web + relay + agctl-бинарь).
# Слои: base (node+тулчейн) → dsh (npm, pinned) → MCP-прогрев → runtime.
# DSH_HOME — ТОЛЬКО volume в рантайме (сессии/креды никогда не в слоях).
# Секреты (LLM-токен) — только env/файл в volume, никогда в образе.
# Linux-сборка agctl кладётся в контекст сборки файлом ./agctl (make image).
FROM node:22-bookworm-slim AS base
RUN apt-get update \
  && apt-get install -y --no-install-recommends python3 python3-pytest python3-pip make g++ ca-certificates curl git \
  && rm -rf /var/lib/apt/lists/* \
  && npm install -g npm@12 --no-audit --no-fund \
  && npm --version
# git нужен агенту в воркспейсе; npm>=11 ради allow-scripts (нативные депы dsh);
# python3+pytest — тулзы доменных шаблонов (например 1c-vibe).

FROM base AS dsh
# идентичность git для агента (в любом репо контейнера)
RUN git config --system user.name "agent" \
  && git config --system user.email "agent@workspace.local" \
  && git config --system init.defaultBranch main \
  && git config --system --add safe.directory '*' \
  && npm config set allow-scripts \
      @deepseek-ai/dsh-subprocess-local,koffi,node-pty,fsevents,protobufjs,esbuild \
      --location=user \
  && npm install -g --no-audit --no-fund @deepseek-ai/dsh@0.1.7-rc.2 \
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
ENV NPM_MCP_CACHE=/opt/npm-mcp
ENV DSH_HOME=/home/dsh
COPY agctl /usr/local/bin/agctl
COPY skel/ /opt/dsh-skel/
COPY entrypoint.sh /usr/local/bin/agctl-entrypoint
COPY relay.mjs /usr/local/bin/agctl-relay
RUN chmod +x /usr/local/bin/agctl /usr/local/bin/agctl-entrypoint /usr/local/bin/agctl-relay && mkdir -p /home/dsh /workspace
WORKDIR /workspace
EXPOSE 3080
ENTRYPOINT ["agctl-entrypoint"]
