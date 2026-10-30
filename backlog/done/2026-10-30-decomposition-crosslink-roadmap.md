---
schema: 1.0.0
id: decomposition-crosslink-roadmap
title: "Кросс-линк профильной декомпозиции в docs/architecture-roadmap.md"
status: done
priority: P3
size: S
area: platform
repo: arijjia/agctl
created: 2026-10-30
updated: 2026-10-30
tags: ["white-label", "docs"]
depends: []
---
## Описание

В кодовом репо направление white-label декомпозиции сейчас видно только из
эпика `profile-decomposition` и штабного дока. Добавить в
`docs/architecture-roadmap.md` (раздел «Следующий горизонт» или «Принципы»)
пункт: ядро — универсальное, кастомизация организации — профиль-данные
(`profiles/`, `--profile`), ссылка на эпик `profile-decomposition` и
`agctl-hq/docs/agctl-decomposition-roadmap.md`. Цель — ночные исполнители и
новые сессии в agctl видят архитектурное направление без чтения штаба.

## Критерии готовности

- В `docs/architecture-roadmap.md` есть пункт о профильной декомпозиции
  со ссылкой на эпик и штабной роадмап; формулировка не противоречит
  принципу №5 (dsh не форкаем).

## Журнал

- 2026-10-30: создано (`gitctl backlog new`); попутный кандидат из работы
  над эпиком `profile-decomposition`.
- 2026-10-30: Сделано: в docs/architecture-roadmap.md добавлен раздел 2.1 «Профильная декомпозиция (white-label)» со ссылками на эпик profile-decomposition, штабной роадмап и docs/customization.md. Коммит вместе с Ф4 агctl.
- 2026-10-30: done — Кросс-линк добавлен в architecture-roadmap.md §2.1 (коммит Ф4)
