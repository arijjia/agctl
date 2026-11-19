---
schema: 1.1.0
id: dsh-bump-next-channel
title: "dsh-bump: следить за тегом next, а не только latest"
status: done
priority: P2
size: S
area: ci
repo: arijjia/agctl
created: 2026-11-13
updated: 2026-11-19
tags: ["dsh", "ci", "deps"]
depends: []
sessions:
  - session-56582fbd-61ce-4d0a-9c89-7afbc5ea2db9:work
---
## Описание

`bin/dsh-bump.sh` и воркфлоу `dsh-bump` сверяют пин dsh с npm dist-tag
`latest`. На 2026-11-13 `latest` = 0.2.2-rc.1, а свежие сборки выходят под
тегом `next` (0.2.2-rc.3): скрипт отставал на две версии и не видел
поломок контракта плагинов. При переходе на rc.3 три поломки
(agentPreset в startSession, штатная русская локаль dsh, русский Preview
Notice) нашлись только e2e-тестами и вручную. Обновления dsh разработчикам
локального режима пойдут через наш конвейер, поэтому ловить такие
изменения нужно раньше.

Сделать:
- `dsh-bump.sh --channel next|latest` (по умолчанию проверять оба и
  сообщать про `next`), `--update` до выбранного канала с той же
  rc-политикой.
- Воркфлоу: на появление новой версии в `next` — собрать образ на ней и
  прогнать `e2e-first-session` и тесты плагинов; результат — в issue или
  уведомление, без автомержа.

## Критерии готовности

- [x] `bin/dsh-bump.sh` показывает и `latest`, и `next`; `--update
      --channel next` поднимает пин до `next`.
- [x] Воркфлоу запускает e2e на версии из `next` и сообщает о поломке.
- [x] Описано в README / docs (процесс обновления dsh).

## Журнал

- 2026-11-13: создано (`gitctl backlog new`).
- 2026-11-19: взято в работу: ветка agent/dsh-bump-next-channel (worktree cit/agctl-dshbump от main 2fba7d9). Пин переехал в ARG DSH_VERSION в Dockerfile (оверрайд --build-arg без правки пина); dsh-bump.sh --channel latest|next (по умолчанию next, rc-политика одна для обоих каналов); img.mk IMG_BUILD_ARGS; воркфлоу dsh-bump: bump (PR по latest) + probe-next/verify-next (образ на next, тесты плагинов internal/provision и e2e-first-session, при провале issue, без автомержа); README и architecture.md описали конвейер.
- 2026-11-19: реализовано и проверено. Пин переехал в ARG DSH_VERSION (Dockerfile); dsh-bump.sh: --channel latest|next (дефолт next, rc-политика общая — dist-tag не отменяет её), --check показывает оба канала и ворачивает 1 по выбранному; img.mk IMG_BUILD_ARGS прокидывает --build-arg в базовые образы; воркфлоу dsh-bump: bump (пин PR'ом по latest, как раньше) + probe-next + verify-next (go vet/test provision, образ IMG_BUILD_ARGS=--build-arg DSH_VERSION=<next> с IMG_FLAVORS=, e2e-first-session с netcat-openbsd; успех — в summary, провал — issue без автомержа); README (§ Обновление dsh) и architecture.md описали конвейер. Проверки: фейк-npm матрица 11 кейсов (drift/prerelease-refusal/оба канала/битые аргументы) — ок; живой npm: дрейф виден (пин rc.3, latest/next rc.5); go vet/test ./... — зелёные; make img_build (теги dshbump-nc) — ок, dsh --version в образе 0.2.2-rc.3; оверрайд-сборка --build-arg DSH_VERSION=0.2.2-rc.5 дала образ с dsh 0.2.2-rc.5 при нетронутом пине; FAKE_LLM e2e-smoke на новом образе PASS=39 FAIL=0. Найдено по ходу: e2e-smoke из worktree падал на профиле utmn из глобального конфига — profiles/ не коммитится (gitignore), подложен symlink на profiles основного чекаута; workflow-джобе verify-next это не грозит (профиль agctl встроен).
- 2026-11-19: done — пин в ARG DSH_VERSION, dsh-bump.sh --channel latest|next, verify-next в воркфлоу (образ на next + тесты плагинов + e2e-first-session, провал — issue); смоук 39/0 на новом образе, оверрайд-сборка rc.5 подтверждена
