---
schema: 1.0.0
id: chat-composer-copy
title: "Chat mode: локализовать composer hint после открытия сессии"
status: done
priority: P2
size: S
area: plugin
repo: arijjia/agctl
created: 2026-10-20
updated: 2026-10-25
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

Русский текст composer сделан штатным locale-контрактом dsh (вместо прямых DOM-патчей, которые React перезаписывал): плагин chat-brand регистрирует язык `ru` (fallback `en`) через `locale.addLanguage` и словарь namespace `conversation` через `locale.register`. Новый пользователь получает русский hint автоматически; явный Language preference в настройках dsh не перетирается. Остальные namespace (sidebar, settings, …) без русского словаря честно падают в dsh-English fallback. Проверка Trajectory-tab в Chat mode не потребовалась: developerTools включается плагином.

## Критерии готовности

- Открытая сессия: composer data-placeholder русский (placeholder.default), без мигания и без возврата английского текста.
- Пустой чат/hero: русский placeholder.hero; выбор воркспейса — placeholder.workspace.
- Прямые setAttribute('data-placeholder'/'placeholder') из плагина удалены.
- Явно выбранный пользователем язык dsh не перезаписывается; отсутствие выбора включает ru.
- Бандл-тест фиксирует locale-контракт; зеркала dsh-skel синхронны.

## Журнал

- 2026-10-20: создано (`gitctl backlog new`).
- 2026-10-20: После активной Session React снова выставляет composer data-placeholder “Message or run a task, / commands, @ files or sessions”; пользовательские add-files/access-mode/right-file/terminal controls скрыты. Прямой DOM setAttribute при boot перезаписывается. Нужен поддерживаемый DSH locale/slot override (или конфигурация), чтобы дать короткий русский текст; не monkey-patch глобальный DOM. Опционально проверить, нужен ли в Chat mode tab Trajectory.
- 2026-10-25: Реализовано: locale.addLanguage(ru→en) + locale.register('conversation','ru',…) в chat-brand; DOM-патчи placeholder удалены; явный preference пользователя не трогаем. node --check + 5/5 bundle-тестов, зеркала dsh-skel синхронны.
- 2026-10-25: done — Русский composer hint через штатный locale dsh
