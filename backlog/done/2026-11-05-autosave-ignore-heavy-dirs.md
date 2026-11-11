---
schema: 1.1.0
id: autosave-ignore-heavy-dirs
title: "Autosave: дефолт-игнор тяжёлых каталогов (.venv, node_modules) и защита от зависаний"
status: done
priority: P2
size: S
area: provision
repo: arijjia/agctl
created: 2026-11-05
updated: 2026-11-11
tags: [autosave, git]
depends: []
epic: ""
sessions:
  - session-e0c9c152-46a8-4490-914f-8ff8e5364e38:work
---
## Описание

Наблюдение 2026-11-05 (живой инстанс, `~/.agctl`): агент в проекте u2 создал
`.venv/` в воркспейсе — autosave каждые 2 минуты начал стабильно падать
(`[project-autosave] u2: local save failed`, тысячи повторов в up.log):
go-git add тянет виртуальное окружение целиком. Пользовательский .gitignore
не обязан это покрывать — платформа должна деградировать грациозно.

## План

1. Дефолт-исключения autosave поверх wt.Excludes: `.venv/`, `venv/`,
   `node_modules/`, `__pycache__/`, `dist/` (свозможностью отключить).
2. Диагностика: писать причину сбоя (index.lock / таймаут) одной строкой
   без путей и контента.
3. Проверить кейс «проект уже сломан»: разовый repair-pass вместо бесконечных
   повторов (backoff после N неудач).

## Критерии готовности

- Воркспейс с `.venv` на гигабайты сохраняется штатно; failures в логе нет.
- Причина сбоя видна в логе, повторы идут с backoff.

## Журнал

- 2026-11-05: создано по факту 3343 подряд падений autosave на живом стенде.
- 2026-11-11: выполнено в worktree wt/autosave-ignore-heavy-dirs: (1) дефолт-исключения .venv/, venv/, node_modules/, __pycache__/, dist/ в AutosaveLocalProject поверх wt.Excludes, отключение/оверрайд AGCTL_AUTOSAVE_HEAVY_EXCLUDES; (2) причина сбоя в лог одной строкой без путей (autosaveFailReason: invalid tree/timeout/index locked/git error); (3) backoff после 3 подряд неудач (экспон. до 1ч) + разовый repair-pass — снятие протухшего .git/index.lock старше 10 мин. Тесты: provision (тяжёлые каталоги не коммитятся, env-оверрайд, repair-pass), server (fail-reason, backoff-порог/сброс). go vet + go test ./... зелёные.
- 2026-11-11: refl: контекста карточки хватило: проблема/симптом/план/критерии полные, сессию-источник не читал — код воспроизводил наблюдение напрямую (excludes в AutosaveLocalProject, тикер server.go). Возврат в карточку: тестируемый критерий «гигабайтный .venv» сжат до юнит-теста на паттерны.
- 2026-11-11: done — autosave не падает на .venv/node_modules: дефолт-исключения, причина сбоя в логе, backoff+repair-pass вместо бесконечных повторов
