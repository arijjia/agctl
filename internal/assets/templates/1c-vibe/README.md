# Рабочее место 1С-разработки (шаблон 1c-vibe на agctl)

Агентный цикл: **Jira → git-ветка → правка выгрузки 1С → самопроверка →
MR с пруфами → человек вмерживает**. Правила — в [AGENTS.md](AGENTS.md).

## Структура

- `scripts/` — тулзы контура (чистый Python, без зависимостей):
  `jira_client.py`, `gitlab_client.py`, `publish.py` (ветка → коммит →
  push → MR → комментарий в Jira), `seed_demo.py`;
- `scripts/demo-project/` — демо-выгрузка конфигурации 1С (XML + BSL) с
  задачами в `tasks/`;
- `skills/1c-vibe-demo/` — пошаговая процедура агента;
- `backlog/` — задачи рабочего места (веди их через `agctl backlog`).

## Настройка (один раз)

    cd scripts
    cp .env.example .env   # заполни JIRA_URL/JIRA_TOKEN, GITLAB_URL/GITLAB_TOKEN

Токены — только в `.env`; в git он не попадает (`.gitignore`).

## Проверка контура

    cd scripts && python3 -m pytest tests/ -q
    python3 jira_client.py get <TASK-ID>   # читаемость Jira

Дальше — скилл `1c-vibe-demo`: попроси агента «выполни задачу INTAIDEV-N».
