# Browser e2e (Playwright)

Изолированные браузерные прогоны продукта. Общая подготовка:

```sh
cd tests/e2e-ui && npm ci && npx playwright install chromium
```

## Сценарии

| Скрипт | Обвязка | Что проверяет |
|---|---|---|
| `cabinet-ui.mjs` | `bin/e2e-cabinet-playwright.sh` | домен → auth → дефолтный чат → Кабинет → заявка → approve → dev-workspace |
| `first-session-ui.mjs` | `bin/e2e-first-session.sh` | D6-регрессия (кейс Б7): чистый браузер → первый запуск → композер активен, сессия открыта, перезагрузка не оставляет в hero, нет фатальных ошибок консоли (`found is not defined`, `setEnabled is not a function`) |

Оба скрипта поднимают собственный `agctl serve` в уникальном TEST_HOME со
своими портами и чистят только те контейнеры, чьи bind-маунты лежат внутри
этого дома. Референсные утверждения — ручной протокол
(`agctl-hq/utmn/rtippo_2026/version_2/dz_1/protokol_ruchnogo_testirovaniya.md`).

Примечание по first-session: «чисто пунктирный» тупик композера
(застрявший выбор воркспейса) детерминированно в e2e не воспроизводится —
он покрыт юнит-тестами лечебника в `dsh-plugins/chat-brand/test/`;
интеграционный кейс ловит регресс сборки по фатальным ошибкам консоли
и активности композера (проверено: красный на e16fce9 / plugin 0.5.0,
зелёный с e956694).
