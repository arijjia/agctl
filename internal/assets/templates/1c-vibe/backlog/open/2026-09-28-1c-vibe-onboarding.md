---
schema: 1.0.0
id: onboarding
title: "Первый шаг: настроить контур и взять демо-задачу"
status: open
priority: P1
size: S
area: 1c-vibe
created: 2026-09-28
updated: 2026-09-28
tags: [onboarding, 1c-vibe]
depends: []
---

## Описание

Проверь готовность контура 1c-vibe и закрой карточку:

1. `cd scripts && cp .env.example .env` — попроси у пользователя токены
   Jira/GitLab (или используй локальный демо-режим без Jira).
2. Прогони тесты: `python3 -m pytest tests/ -q`.
3. Возьми демо-задачу из `scripts/demo-project/tasks/demo-tasks.md` и
   пройди скилл `1c-vibe-demo` до MR.

## Критерии готовности

- тесты зелёные; демо-задача дошла до MR (или честно описан блокер в
  карточке);

## Журнал

- 2026-09-28: карточка создана шаблоном 1c-vibe (agctl).
