---
schema: 1.0.0
id: corporate-qwen-defaults
title: "make up по умолчанию настраивает корпоративные Qwen-модели"
status: in-progress
priority: P2
size: S
area: platform
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: []
depends: []
epic: v3-unified-runtime
---
## Описание

Новые chat-home должны сразу видеть корпоративные Qwen-модели через LiteLLM,
как прежний пилот, без пустого model catalog и повторного DSH Get started.
Base URL/model list — безопасные публичные defaults; API token остаётся raw
secret в `.env`/env и не попадает в репозиторий. `make up` предупреждает, если
секрет пуст или содержит header-invalid control characters.

## Критерии готовности

- `agctl serve` и `make up` по умолчанию используют
  `https://litellm-dev.utmn.ru/v1`, Qwen model list и `qwen-3.8-fast` default.
- Env overrides продолжают работать; API token не логируется и принимает только
  raw single-line value (trim внешних пробелов, ошибка на CR/LF/control chars).
- `.env.example` ясно объясняет настройку токена; make up печатает предупреждение
  без секрета, если он не задан.
- Тесты фиксируют дефолты/валидацию без настоящего API-токена.

## План

- Установить корпоративные defaults в serve и make; исправить typo в qwen list.
- Документировать `.env` и raw-key формат, прогнать cmd/server suite.


## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
