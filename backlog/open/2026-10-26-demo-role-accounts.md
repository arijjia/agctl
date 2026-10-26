---
schema: 1.0.0
id: demo-role-accounts
title: "Локальные тестовые учётки user/developer/admin с README"
status: in-progress
priority: P2
size: S
area: meta
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
epic: night-chat-polish-2026-10-22
---
## Описание

Для ручной проверки role-gated UI нужны отдельные учётки user/developer/admin.
Создавать их только явной dev-командой после реального первого admin-входа;
serve не должен автоматически добавлять известные пароли в любую базу.

## Критерии готовности

- `make demo-users` идемпотентно создаёт `demo-user`, `demo-developer`,
  `demo-admin` с соответствующими ролями и не меняет существующие логины.
- Команда отказывается работать на пустой базе, чтобы первый реальный аккаунт
  сохранял bootstrap-admin поведение.
- README содержит логины/пароли и явное предупреждение: только localhost/dev,
  не публиковать сервер с этими credentials.
- CLI/store tests проверяют роли и идемпотентность.

## План

- Добавить явную команду `agctl seed-demo-users` и `make demo-users`.
- Сидировать после регистрации владельца, протестировать три роли и записать
  локальные credentials в README.


## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
