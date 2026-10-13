---
schema: 1.0.0
id: dsh-pin-bump-automation
title: "Обновить DSH pin и настроить e2e-gated PR для новых версий"
status: in-progress
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-10-13
updated: 2026-10-13
tags: ["night"]
depends: []
epic: night-backlog-sweep-2026-10-12
---
## Описание

Связанный контракт: `arijjia/agctl-hq/backlog/open/2026-10-12-dsh-pin-bump-automation.md`.
В образе DSH pin поднят до `0.2.0-rc.2`. Нужна воспроизводимая проверка
совместимости и автоматизация дальнейших rc/stable обновлений: локальная команда
обновляет pin, scheduled GitHub workflow собирает образ и гоняет изолированный
e2e на фейковом LLM, затем создаёт PR только при успешных проверках.

## План

- `scripts/dsh-bump.sh --check|--update`: сверка npm dist-tags, alpha/beta/next
  отклонять; update менять ровно один pinned Dockerfile entry.
- Weekly/workflow_dispatch job: обновить → go vet/test → SPA build → Docker build
  → e2e-smoke (CI fake LLM bind) → PR.
- Проверить DSH 0.2.0 создание нового воркспейса, provider import, mint, autosave,
  stop/start; отдельно проверить апгрейд старого `0.1.7-rc.2` dsh-home с сессиями.

## Критерии готовности

- Пин `0.2.0-rc.2` проходит изолированный e2e; рабочий провод контейнер→fake LLM,
  mint и сохранность проверены.
- `--update` на тестовом Dockerfile меняет старую версию на npm latest rc/stable;
  `--check` ловит drift; alpha не принимается.
- Scheduled workflow запускает build/e2e и создаёт PR только при новых версиях.
- Legacy `0.1.7-rc.2` DSH_HOME с существующей сессией поднимается на новом образе
  без потери данных — пока НЕ проверено, блокирует полное закрытие карточки.

## Не делать

- Не ставить alpha/latest плавающим в Dockerfile; не мигрировать dsh-home
  принудительно при запуске пользователя.

## Журнал

- 2026-10-13: создано (`gitctl backlog new`).
- 2026-10-14: pin `0.2.0-rc.2`; образ собран; новый воркспейс e2e PASS 35/0,
  CHAT=PASS через host-gateway; `settings.yaml` импортируется и переименовывается
  DSH в `settings.yaml.imported`; `scripts/dsh-bump.sh --check|--update` + weekly
  e2e-gated PR workflow добавлены. Legacy upgrade 0.1.7→0.2.0 с историей сессии
  отдельно не проверен; статус оставить in-progress.
