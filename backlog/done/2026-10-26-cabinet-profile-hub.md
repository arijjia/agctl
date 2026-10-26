---
schema: 1.0.0
id: cabinet-profile-hub
title: "Перенести Кабинет в нижний профильный popup"
status: done
priority: P2
size: M
area: plugin
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
epic: night-chat-polish-2026-10-22
---
## Описание

Убрать отдельную панель Кабинета и перенести профиль/кабинет в чип пользователя
внизу штатного сайдбара. Popup содержит профиль, native DSH Settings и справку;
профиль показывает понятную роль с описанием прав, доступную статистику
provider-reported токенов/активности, role-request и SSH. Рантайм управляется
только agctl serve; пользовательские кнопки запуска/останова/логов удалить.

## Критерии готовности

- Внизу sidebar видны круглый avatar и login/display name; popup открывается над
  чипом и содержит «Профиль», native «Настройки», «Справка».
- Кабинет/профиль — один overlay; отдельного panellist entry, workspace list и
  workspace-create формы нет.
- Роль показана в отдельном блоке «Права доступа» с локальным названием и
  пояснением; роли user/developer/admin проверены.
- Статистика показывает доступные tokenUsage totals/breakdown из DSH session
  projections и heatmap активности за год. Не выдавать activity sessions за tokens/day.
- SSH key остаётся в профиле developer/admin; заявки пользователя и решения
  admin сохраняются.
- UI и API не содержат start/stop/logs controls; `/api/me/runtime` только
  read-only status/sshPort для аутентифицированного пользователя.
- Кабинет plugin/test/docs и embedded mirror синхронны.

## План

- Удалить отдельный Cabinet sidebar panel и перенести popup в user-chip.
- Собрать OpenAI-like profile overlay: avatar, stats/heatmap, role, requests, SSH.
- Удалить runtime controls и дублирующий Workspaces list; оставить help popup.
- Переписать tests/docs, синхронизировать embedded profile.


## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
- 2026-10-26: Готово: панель Кабинет демонтирована; чип профиля зарегистрирован в штатном list-слоте sidebar.footer.action (wide из ownerProps, без MutationObserver); popup Профиль/Настройки/Справка; профиль — OpenAI-like: avatar, роль с описанием прав, tokenUsage-статистика из DSH projections, годовая heatmap, SSH, заявки. Runtime API урезан до read-only GET /api/me/runtime; start/stop/logs удалены (404/405), тесты обновлены.
- 2026-10-26: done — Профиль-плашка вместо панели Кабинета
