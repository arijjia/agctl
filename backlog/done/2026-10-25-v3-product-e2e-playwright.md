---
schema: 1.1.0
id: v3-product-e2e-playwright
title: "Playwright e2e v3: роль developer → проект-папка → сессия в проектном cwd → SSH"
status: done
priority: P2
size: M
area: plugin
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-11-15
tags: ["e2e", "playwright", "v3"]
depends: []
sessions:
  - session-cc649d5c-51d5-4a5b-b2d0-fabbb52f8752:work
---
## Описание

Сквозной браузерный тест продуктового потока v3: обычный пользователь подаёт
заявку на роль developer, админ одобряет, developer сохраняет SSH-ключ,
рантайм перезапускается (ключ применяется при следующем старте), создаётся
проект-папка через штатный диалог «Новый воркспейс», сессия работает в
проектном cwd, SSH-канал проверяется настоящим key-only входом в контейнер.

## Критерии готовности

- `bin/e2e-developer-flow.sh` + `tests/e2e-ui/developer-flow-ui.mjs`: полный
  поток зелёный на локальном Docker (register → role-request → approve →
  pubkey → restart → cabinet SSH overlay с живым портом → проект → сессия →
  ssh root@… -p N key-only → /workspace/<проект> с локальным .git).
- Тест ловит регрессию перехвата нативной кнопки добавления воркспейса
  (должен открываться диалог agctl, а не контейнерный DirectoryPicker).

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-11-15: сделано. Попутно найден и починен продуктовый баг: dsh 0.2.2
  локализовал нативную кнопку в «Добавить рабочую область», и перехват
  chat-brand по `/^add workspace$/i` перестал срабатывать — из UI создавался
  воркспейс встроенным DirectoryPicker вместо диалога agctl с каталогом
  возможностей. Фикс + юнит-тест в chat-brand (оба зеркала), поймано новым
  e2e. Сценарий e2e: два контекста Playwright (developer/admin), API-шаги
  same-origin fetch, admin restart контейнера через /api/admin/containers,
  wrapper довалидирует SSH (whoami, /workspace/<проект>, .git).
- 2026-11-15: refl: e2e вскрыл продуктовую регрессию dsh 0.2.2 (RU-локализация отключила перехват кнопки добавления воркспейса) — фикс сделан в этой карточке; стаб-карточки бэклога (без описаний) мешают планированию; cabinet-ui.mjs остался от старого UX кабинета и демонттированного потока заявок
- 2026-11-15: done — developer-flow e2e: register → заявка → approve → SSH-ключ → admin-рестарт рантайма → cabinet SSH overlay с живым портом → проект через штатный диалог → сессия в проектном cwd → реальный SSH key-only вход; попутно починен перехват кнопки добавления воркспейса в chat-brand (RU-локализация dsh 0.2.2)
