# agctl — правила агентов в этом репо

## Продукт

Платформа агентских рабочих мест: одно Go-ядро и три бинаря — `agctl`
(локальный режим разработчика), `agctl-server` (сервер организации, рантаймы
docker | k8s | ax), `agctl-guest` (внутри образа рабочего места); Docker,
SQLite, DeepSeek Harness в контейнере рабочего места. Публичный репозиторий.

## Конвенции работы

- Код правим по одной задаче в своей ветке (`agent/<тема>` или `masha/<тема>`),
  в `main` напрямую не коммитим; пуш — только по решению владельца.
- Один коммит = одна задача; формат истории — как в соседних репо.
- Секреты (LLM-токены, ключи) никогда не попадают в код, тесты, логи и образы;
  CI-гард gitleaks. Тестовые прогоны — только в изолированном `$TEST_HOME`
  с уникальным тегом образа; Docker e2e — `bin/e2e-local-mode.sh`,
  `bin/e2e-smoke.sh`, `bin/e2e-cabinet.sh`, `bin/e2e-ssh.sh`; браузерный — `tests/e2e-ui` (Playwright).
- Документация в репо: README — установка и запуск всех трёх режимов,
  `docs/architecture.md` — архитектура всей системы (режимы, сети и порты,
  данные и секреты, ревью кодовой базы по режимам). Прежние доки (архитектура
  v3, Kubernetes, кастомизация, безопасность, capacity, сборка и CLI) —
  архив в `agctl-hq/docs/`; стратегия — штаб
  (`agctl-hq/docs/agctl-platform-concept-v2.md`,
  `agctl-hq/docs/agctl-sandbox-and-runtime-theory.md`).
- Перед финалом: `go vet ./... && go test ./...`, SPA собирается
  (`make web_build`), при изменении образа — `make img_build` и e2e-смоук.
- Тулинг бэклога — `gitctl backlog ...`; карточки задач — `backlog/open/`.

<!-- backlog: begin (managed by ~/.agents/skills/backlog) -->
## End-of-tour backlog reflection

After completing work in this repository, briefly recommend 1–3 useful backlog candidates. Phrase each as a recommendation describing what to track and why; do not put questions inside the recommendation. If this repository has no `backlog/`, recommend adding one when ongoing work merits it.
<!-- backlog: end (managed by ~/.agents/skills/backlog) -->
