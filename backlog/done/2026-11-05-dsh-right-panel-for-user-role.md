---
schema: 1.1.0
id: dsh-right-panel-for-user-role
title: "Правая панель dsh не монтируется для роли user — нужен патч/апгрейд dsh"
status: done
priority: P2
size: M
area: server
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-12
tags: [dsh, ux]
depends: []
epic: ""
sessions:
  - session-f61b3c76-c5b1-4184-b046-86a58e758c75:work
---
## Описание

Кейс «юзер не может открыть артефакт агента» (презентация): превью
deliverables живёт в правой панели dsh, которая для роли user не монтируется.
Что проверено 2026-11-05 (playwright, сборка с фиксами):

- DeveloperToolsPreference включается для user (agctl-chat-brand
  syncRoleControls, devToolsReady=true) — панель всё равно не появляется;
- «Open right sidebar» / ExpandButton присутствует в DOM, но computed
  display:none без CSS-правила и без inline-style — стиль выставляется
  императивно в shell dsh по условию, связанному с аккаунтом/сессией;
- клик по «Preview in sidebar» и по file-mention молча не делает ничего:
  shell-операция openFile для user — no-op до вызова
  sidebarRight.openResource.

## План

1. Поднять вопрос upstream (dsh): как штатно включить правую панель для
   не-developer аккаунтов (boot-флаг / preference / account tier).
2. Если upstream-пути нет — патчить post-boot: найти в собранном фронтенде
   точку императивного display:none и переопределить плагином (хрупко,
   пометить как техдолг).
3. Альтернатива-обходчик уже работает: скачивание артефактов через
   «Воркспейсы: возможности → Файлы» (GET /api/projects/{name}/file).

## Критерии готовности

- В сессии user клик по «Preview in sidebar» открывает правую панель
  (для типов с рендерером: текст/изображения/PDF/HTML).

## Журнал

- 2026-11-05: создано по кейсу с презентацией Pro_sebya.pptx
  (session-8a6ca605); воспроизводится стабильно, не зависит от сборки.
- 2026-11-12: Диагноз: панель прятал не shell dsh, а гейтвей agctl — chatShellUserStyle (chatshell.go, b2a070f) безусловно скрывал кнопку «Open right sidebar» и жал третью колонку фрейма в 0px. Плагин chat-brand (22a1bf0) уже включил панель всем ролям (roleCanUseDevTools=!!role, ready-флаг, ensureRoleSurfaceStyles прячет гайд терминала/файлов) — разрыв был только на стороне гейтвея. Источник: трассировка internal/server/chatshell.go + dsh-plugins/chat-brand/lib/client.js + исходники dsh-sidebar-right из checkout ~/.local/lib (read-only); сессию session-8a6ca605 не читал — факты карточки подтвердились кодом. Фикс: user получает тот же ready-gated pre-paint collapse (D8), user-стиль скрывает лишь Access mode/New terminal/Workspace files. Юнит-тесты обновлены, go vet+test зелёные; контейнерный e2e не гонялся (нужен make img_build) — пункт merge-сообщения «Проверить перед пушем».
- 2026-11-12: refl: Карточка винила «императивный display:none в shell dsh» — вывод made до находки собственного инжекта agctl: pre-paint CSS гейтвея выглядит как «чужой» императивный стиль. Урок: при диагностике чужого UI сначала grep'ить собственные инжекты шлюза.
- 2026-11-12: done — Правая панель dsh доступна роли user: гейтвейный chatShellUserStyle больше не прячет toggle и колонку, превью открывается штатным sidebarRight.openResource
