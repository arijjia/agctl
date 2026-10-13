# agctl — правила агентов в этом репо

## Продукт

Платформа агентских рабочих мест: один Go-бинарь (`agctl serve`), Docker,
SQLite, DeepSeek Harness в контейнере воркспейса. Публичный репозиторий.

## Конвенции работы

- Код правим по одной задаче в своей ветке (`agent/<тема>` или `masha/<тема>`),
  в `main` напрямую не коммитим; пуш — только по решению владельца.
- Один коммит = одна задача; формат истории — как в соседних репо.
- Секреты (LLM-токены, ключи) никогда не попадают в код, тесты, логи и образы;
  CI-гард gitleaks. Тестовые прогоны — только в изолированном `$TEST_HOME`
  с уникальным тегом образа (см. `agctl-hq/docs/agctl-mvp-e2e-test-guide.md`).
- Перед финалом: `go vet ./... && go test ./...`, SPA собирается
  (`make web_build`), при изменении образа — `make img_build` и e2e-смоук.
- Тулинг бэклога — `gitctl backlog ...`; карточки задач — `backlog/open/`.

<!-- backlog: begin (managed by ~/.agents/skills/backlog) -->
## End-of-tour backlog reflection

After completing work in this repository, briefly recommend 1–3 useful backlog candidates. Phrase each as a recommendation describing what to track and why; do not put questions inside the recommendation. If this repository has no `backlog/`, recommend adding one when ongoing work merits it.
<!-- backlog: end (managed by ~/.agents/skills/backlog) -->
