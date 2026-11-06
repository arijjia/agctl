---
schema: 1.0.0
id: dsh-right-panel-for-user-role
title: "Правая панель dsh не монтируется для роли user — нужен патч/апгрейд dsh"
status: open
priority: P2
size: M
area: server
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-05
tags: [dsh, ux]
depends: []
epic: ""
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
