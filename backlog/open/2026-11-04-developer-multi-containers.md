---
schema: 1.1.0
id: developer-multi-containers
title: "Лаунчер: несколько параллельных контейнеров на одного developer (редкий кейс)"
status: open
priority: P3
size: L
area: server
repo: arijjia/agctl
created: 2026-11-04
updated: 2026-11-04
tags: ["developer", "launcher", "multi-runtime"]
depends: []
sessions:
  - session-4e3c026b-5dcd-4cb7-96f8-7b7c83085443:work
---
## Описание

Владелец (2026-11-04): «возможно, будет возможность редким пользователям по
нескольку контейнеров запускать». v1 лаунчера (developer-launcher-images)
даёт один персональный runtime со сменой образа; эта карточка — про
настоящий мульти-рантайм: несколько живых контейнеров одного developer с
разными образами и общим или раздельным томом (решить), выбор активного в
UI. Затрагивает контракт ChatProvider (RuntimeID per (user,slot)), StorageID
(том per slot), TTL-рипер (несколько lease на юзера) и квоты контейнеров
(лимит slot'ов на роль).

## Критерии готовности

- [ ] дизайн-решение: том общий vs per-slot; лимит слотов по роли
- [ ] ChatProvider: EnsureChat(slot-spec), лаунчер видит все слоты
- [ ] TTL-рипер и админ-панель видят слоты как отдельные контейнеры

## Не делать

- Не ломать модель «один runtime на user» для роли user.

---
## Журнал

- 2026-11-04: создано (`gitctl backlog new`).
