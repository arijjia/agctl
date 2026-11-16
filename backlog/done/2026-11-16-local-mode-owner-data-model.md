---
schema: 1.0.0
id: local-mode-owner-data-model
title: "Локальный режим без серверной модели данных: один владелец без users/ролей/пароля"
status: done
priority: P3
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-16
updated: 2026-11-16
tags: ["local", "architecture"]
depends: ["split-binaries-local-server"]
---
## Описание

Локальный режим поднимает полную серверную модель: SQLite с users,
sessions, ролями и 12+ миграциями, владельца-developer'а со случайным
паролем, `requireUser` на каждом запросе. Он изображает сервер с одним
пользователем. После разделения бинарей (`split-binaries-local-server`)
можно дать локальному режиму собственную простую модель: один владелец,
сессия по одноразовому токену, настройки в `dev.yaml`, без таблиц ролей и
заявок.

## Критерии готовности

- [x] Решено, что из store реально нужно локально (sessions, audit,
      settings) и что заменяется файлом/памятью.
- [x] Владелец без строки в users и без пароля; ветки `isLocal()` в
      ядре сведены к минимуму.
- [x] Миграция существующего `~/.agctl-dev` без потери сессий dsh.

## Журнал

- 2026-11-16: создано по итогам ревью архитектуры; брать после разделения
  бинарей.
- 2026-11-16: Решено: SQLite локально остаётся хранилищем общего кода ядра для settings (образ, провайдер, MCP/компоненты проектов) и audit_log — у них нет FK на users; users и sessions локально не используются. Сделано (internal/server/local_owner.go): владелец — не учётка (нет строки в users, нет пароля, роль developer из конфигурации — roleOf), ID localOwnerID=1, на прежней установке — ID её строки owner (localOwnerIDFor: тот же chats/u<ID>, сессии dsh целы); сессии браузера — data/local-sessions.json 0600 (sha256 токенов + сроки), переживают рестарт; после перехода старая cookie недействительна — один раз agctl dev open. initLocal больше не создаёт учётку и не правит роль; userRole/developerSSHPubKey — через roleOf; новых веток isLocal() не добавлено. Тесты: TestLocalOwnerIsNotAnAccount, TestOwnerSessionsSurviveRestart, TestLocalOwnerKeepsLegacyRowID; go vet/test; e2e-local-mode 44/0.
