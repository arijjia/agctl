---
schema: 1.1.0
id: dsh-image-bump-022-rc3
title: "Пин dsh в образе чата: 0.2.1-rc.5 → 0.2.2-rc.3"
status: done
priority: P2
size: M
area: runtime
repo: arijjia/agctl
created: 2026-11-13
updated: 2026-11-13
tags: ["dsh", "image", "deps", "locale"]
depends: []
---
## Описание

`internal/assets/image/Dockerfile` ставил `@deepseek-ai/dsh@0.2.1-rc.5`.
Поднять до самой свежей сборки 0.2.2-rc.3 (dist-tag `next`; `latest` на
npm — rc.1, поэтому `bin/dsh-bump.sh --update` до неё не дотягивается).

## Критерии готовности

- [x] Пин 0.2.2-rc.3, `dsh --version` в образе = 0.2.2-rc.3
- [x] `bin/e2e-first-session.sh` зелёный (первый запуск + reload)
- [x] `bin/e2e-smoke.sh` FAKE_LLM=1: 37 PASS, 0 FAIL
- [x] Бренд, подсказка композера и текст удаления воркспейса — платформенные

## Журнал

- 2026-11-13: done. Поломки контракта rc.3 и фиксы в chat-brand:
  (1) `agentPreset` в `startSession` теперь пресет агента, «danger-full-access»
  неизвестен → первая сессия не создавалась (композер неактивен). Плагин
  больше не шлёт agentPreset: режим роли user задаёт сервер
  (`permission.defaultPreset`, provision.EnsurePermissionDefaults) — на
  живом стенде user = «Полный доступ».
  (2) dsh несёт свой ru: `locale.register("conversation"|"workspace","ru")`
  бросает «already has locale» → наши ключи точечно подменяются в штатном
  словаре (overrideLocaleEntries, откат при выгрузке). Критично для
  `delete.desc`: штатный текст обещает «папка сохранится», у agctl удаление
  стирает каталог.
  (3) Preview Notice и hero-слоган («В неизвестность») — русские варианты.
  e2e-first-session: селектор чипа под русские aria-label rc.3 (и не
  путает с «Действия рабочей области»). Проверено вживую: developer,
  glm-5.3-flash ответила, диалог удаления с текстом agctl.
