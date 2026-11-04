---
schema: 1.1.0
id: litellm-quota-enforcement
title: "Квоты токенов: инжект виртуальных ключей LiteLLM по бюджетам из админки"
status: open
priority: P2
size: M
area: server
repo: arijjia/agctl
created: 2026-11-04
updated: 2026-11-04
tags: ["quotas", "litellm", "enforcement"]
depends: []
sessions:
  - session-4e3c026b-5dcd-4cb7-96f8-7b7c83085443:work
---
## Описание

Квоты (админка admin-panel-super-admin, v1) хранятся в SQLite и показываются,
но не применяются: LLM-трафик ходит из контейнеров напрямую в LiteLLM с одним
платформенным ключом. Применение: agctl при старте рантайма минтит
персональный виртуальный ключ LiteLLM (POST /key/generate) с budget_duration
и max_budget из действующей квоты пользователя (персональная перекрывает
ролевую) и инжектит его вместо платформенного. Конфиг: URL+ключ admin-API
LiteLLM (env/профиль, флаг включения). Без конфигурации — текущее поведение.

## Критерии готовности

- [ ] конфиг admin-API LiteLLM (env/profile), флаг включения
- [ ] EnsureChat инжектит персональный виртуальный ключ с бюджетом из quotas
- [ ] отзыв/ротация ключа при смене квоты и при удалении пользователя
- [ ] тесты на маппинг квота→ключ (fake LiteLLM httptest)

## Не делать

- Не хранить admin-ключ LiteLLM в образе/гите.

---
## Журнал

- 2026-11-04: создано (`gitctl backlog new`).
