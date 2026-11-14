# agctl-dsh: рабочее место агента (dsh web на eth0 + agctl-бинарь).
# Слои: base (node+тулчейн) → dsh (npm, pinned) → MCP-прогрев → runtime.
# DSH_HOME — ТОЛЬКО volume в рантайме (сессии/креды никогда не в слоях).
# Секреты (LLM-токен) — только env/файл в volume, никогда в образе.
# Linux-сборка agctl кладётся в контекст сборки файлом ./agctl (make image).
FROM node:22-bookworm-slim AS base
RUN apt-get update \
  && apt-get install -y --no-install-recommends python3 python3-pytest python3-pip make g++ ca-certificates curl git openssh-server iptables \
  && rm -rf /var/lib/apt/lists/* \
  && npm install -g npm@12 --no-audit --no-fund \
  && npm --version
# iptables — egress-allowlist: хелпер применения DOCKER-USER правил (egress.go);
# git нужен агенту в воркспейсе; npm>=11 ради allow-scripts (нативные депы dsh);
# python3+pytest+pip — тулзы доменных шаблонов (например 1c-vibe) и надстроек
# профилей: пакеты организации ставятся в её image/Dockerfile (FROM agctl-dsh).

FROM base AS dsh
# идентичность git для агента (в любом репо контейнера)
RUN git config --system user.name "agent" \
  && git config --system user.email "agent@workspace.local" \
  && git config --system init.defaultBranch main \
  && git config --system --add safe.directory '*' \
  && npm config set allow-scripts \
      @deepseek-ai/dsh-subprocess-local,koffi,node-pty,fsevents,protobufjs,esbuild \
      --location=user \
  && npm install -g --no-audit --no-fund @deepseek-ai/dsh@0.2.2-rc.3 \
  && dsh --version
ENV PATH="/opt/dsh/bin:${PATH}"

FROM dsh AS mcp-warm
# Прогрев npx-кэша MCP-серверов каталога (provision.PlatformMcpServers): первая
# сессия не тянет сеть. Успех — пакет лёг в кэш: серверы без конфига (токен
# GitLab и т.п.) сразу выходят с ошибкой, это не провал прогрева.
ENV NPM_MCP_CACHE=/opt/npm-mcp
RUN mkdir -p "$NPM_MCP_CACHE" \
  && for pkg in duckduckgo-mcp-server @upstash/context7-mcp @zereight/mcp-gitlab@2.3.27 @playwright/mcp@0.0.85; do \
       echo "prewarm: $pkg"; \
       timeout 60 npx --yes --cache "$NPM_MCP_CACHE" "$pkg" </dev/null >/dev/null 2>&1 || true; \
       name="${pkg%@*}"; [ -n "$name" ] || name="$pkg"; \
       ls -d "$NPM_MCP_CACHE"/_npx/*/node_modules/"$name" >/dev/null 2>&1 \
         || { echo "prewarm FAIL $pkg: нет в кэше"; exit 1; }; \
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
# Инструменты разработчика (локальный режим, роль developer): поиск, JSON,
# клиенты БД, CLI GitLab (glab) и Jira (jira-cli), uv/uvx (Python-проекты и
# MCP Jira через uvx). Архитектура — из dpkg: собирается и BuildKit, и
# классическим билдером (agctl serve EnsureImage). Слой после LibreOffice:
# смена версий не инвалидирует dsh/MCP.
ARG GLAB_VERSION=1.130.0
ARG JIRA_CLI_VERSION=1.7.0
ARG UV_VERSION=0.13.8
RUN apt-get update \
  && apt-get install -y --no-install-recommends ripgrep jq fd-find sqlite3 postgresql-client \
       default-mysql-client redis-tools tree htop unzip zip less nano procps \
  && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
  && rm -rf /var/lib/apt/lists/* \
  && arch="$(dpkg --print-architecture)" \
  && curl -fsSL -o /tmp/glab.deb "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_linux_${arch}.deb" \
  && dpkg -i /tmp/glab.deb && rm /tmp/glab.deb \
  && jarch="$([ "$arch" = amd64 ] && echo x86_64 || echo arm64)" \
  && curl -fsSL "https://github.com/ankitpokhrel/jira-cli/releases/download/v${JIRA_CLI_VERSION}/jira_${JIRA_CLI_VERSION}_linux_${jarch}.tar.gz" | tar -xz -C /tmp \
  && install -m 0755 "/tmp/jira_${JIRA_CLI_VERSION}_linux_${jarch}/bin/jira" /usr/local/bin/jira && rm -rf /tmp/jira_* \
  && uarch="$([ "$arch" = amd64 ] && echo x86_64 || echo aarch64)" \
  && curl -fsSL "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-${uarch}-unknown-linux-gnu.tar.gz" | tar -xz -C /tmp \
  && install -m 0755 "/tmp/uv-${uarch}-unknown-linux-gnu/uv" "/tmp/uv-${uarch}-unknown-linux-gnu/uvx" /usr/local/bin/ \
  && rm -rf /tmp/uv-* \
  && glab --version && jira version && uv --version
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
# Google AX / Agent Substrate: PID 1 песочницы — /usr/local/bin/ax-task-runner
# (контракт раннера AX); здесь это обёртка над `agctl ax-runner`: probes на
# :80, прокси на dsh web :3080, хвост лога запуска для шлюза. В Docker/K8s не
# используется (ENTRYPOINT ниже).
RUN printf '#!/bin/sh\nexec /usr/local/bin/agctl ax-runner "$@"\n' > /usr/local/bin/ax-task-runner && chmod +x /usr/local/bin/ax-task-runner
WORKDIR /workspace
EXPOSE 3080 2222
# Живость dsh: любой HTTP-ответ (в т.ч. 401/303 без cookie) = процесс жив и
# слушает dsh web на eth0-адресе контейнера (agctl-dsh-net health); обрыв/
# таймаут/5xx = unhealthy. Start-period — с запасом на первый прогрев
# (MCP-кэш уже вшит). Статус читает agctl (reconcile).
HEALTHCHECK --interval=30s --timeout=6s --start-period=150s --retries=3 \
  CMD node /usr/local/bin/agctl-dsh-net health
ENTRYPOINT ["agctl-entrypoint"]
