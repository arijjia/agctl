# agctl-dsh-web — вкус образа для веб-разработки поверх базового agctl-dsh:
# Playwright с Chromium (и системными зависимостями) для MCP Playwright
# (@playwright/mcp, каталог MCP agctl) и e2e-тестов проектов. Сборка:
# make img_build (тег agctl-dsh-web:<тег>), BASE — базовый образ.
ARG BASE=agctl-dsh:0.2.2
FROM ${BASE}
# Браузеры — в общий путь (не в $HOME пользователя контейнера). Ставит их тот
# playwright, что лежит в прогретом npx-кэше @playwright/mcp (версии браузера
# и драйвера обязаны совпадать).
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/ms-playwright
RUN pw="$(find "$NPM_MCP_CACHE/_npx" -path '*/node_modules/playwright/cli.js' | head -1)" \
  && test -n "$pw" \
  && node "$pw" install --with-deps chromium \
  && rm -rf /var/lib/apt/lists/* \
  && ls /opt/ms-playwright
