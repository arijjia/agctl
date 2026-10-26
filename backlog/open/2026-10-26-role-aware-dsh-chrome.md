---
schema: 1.0.0
id: role-aware-dsh-chrome
title: "User видит только Settings; developer/admin сохраняют правую DSH-панель и пресеты"
status: in-progress
priority: P1
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

Спрятать AccountMenu из dsh (More/Feedback/Sign in/Get started), оставить
fallback native Settings. Обычная роль получает right pane, скрытую до первого
кадра, и только Standard preset; developer/admin — полный исходный dsh right
pane и все built-in presets (Standard/PTC/Minimal). Решение должно опираться на
agctl role, а не на факт наличия SSH-ключа.

## Критерии готовности

- Chat-home Cordis patch отключает только `ui-settings-account`: More/Feedback/
  Sign in/Get started исчезают, нативный Settings trigger остаётся.
- Для `user` right sidebar скрывается pre-paint без мигания, Developer Tools
  выключены, preset picker показывает только Standard.
- Для `developer` и `admin` right sidebar/terminal/files и native presets остаются
  доступны; account plugin не затирает их настройки.
- Профиль/role API и host `chatShell` определяют роль; неизвестная ошибка auth
  не открывает privileged UI.
- Go/plugin tests покрывают user/developer/admin и account-menu override.

## План

- Добавить profile patch `disabled: true` для `ui-settings-account` только в
  EnsureChatPlugins (не в общий dev skeleton).
- Ролево выбирать pre-paint CSS `chat-shell-user`; убрать безусловный client
  collapse/hideDevPanels; DeveloperTools sync по `/api/me`.
- Проверить native preset picker: он уже фильтрует PTC/Minimal по
  `developerTools.enabled`; закрепить это тестами.


## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
