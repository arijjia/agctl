---
schema: 1.1.0
id: dsh-image-bump-rc5
title: "Пин dsh в образе чата: 0.2.1-rc.2 → 0.2.1-rc.5+"
status: done
priority: P3
size: M
area: runtime
repo: arijjia/agctl
created: 2026-11-04
updated: 2026-11-04
tags: ["dsh", "image", "deps"]
depends: []
sessions:
  - session-a90b5b43-2023-4178-9aa8-eea3b4162b13:work
---
## Описание

`internal/assets/image/Dockerfile` ставит `@deepseek-ai/dsh@0.2.1-rc.2`
(образ `agctl-dsh:0.2.2`). В rc.2 живут те же классы UI-гонок, которые
сложились в дефект D6: restoration-навигация оставляет пользователя без
сессии, `layout.selectPanel`/`beginNavigation` отменяют чужую навигацию
без отмены создания. Апстрим это правил (см. теги до 0.2.2-alpha) —
держаться двух rc позади нет причин, а собственные компенсаторы
(`ensureFirstWorkspaceSession`, `healStuckWorkspacePick`) при апгрейде
должны стать просто лишними, но безвредными.

Риски апгрейда — контрактные: shape `ctx.configForms.developerTools`
(`setEnabled` на preference против observable — уже было источником
TypeError), boot-граф `dsh.client.inject` (доставка бандла ui-workspace),
DOM-селекторы брендинга в chatshell/chat-brand.

## План

1. `bin/dsh-bump.sh` → новый пин (0.2.1-rc.5 как минимальная цель;
   оценить 0.2.2-alpha по changelog), пересборка образа `make img_build`.
2. Контракт-аудит против нового dsh: grep-чек API из chat-brand 0.6.0
   по исходникам тега (checkout deepseek-harness): `uiWorkspace.startSession`,
   `configForms.developerTools.{enabled,setEnabled}`, `layout.closeRightbar`.
3. Сквозняк: e2e-смоук + browser e2e на изолированном стенде; при
   наличии карточки e2e-first-session-regression — прогнать и её
   (она и есть главный предохранитель).
4. Скелет dsh-home (`internal/assets/dsh-skel`) сверить с новым профилем
   web: дрейф структур (profiles/web/package.json, cordis) чинит
   `make profile-*` / dsh-bump, проверить тесты сkeleton-гарды
   (`internal/assets/assets_test.go`).

## Критерии готовности

- Образ собран с новым пином; `docker exec <chat> dsh --version`
  подтверждает версию.
- `go test ./...`, `node --test dsh-plugins/*/test/`, e2e-смоук — зелёные.
- Живой стенд владельца обновлён и повторно прогонены кейсы Б7–Б9
  протокола (обновить статусы в agctl-hq).

## Не делать

- Не прыгать через alpha-версии с ломающими изменениями без
  контракт-аудита; при-breaking — отдельная карточка на адаптацию.
- Не чинить обнаруженные апстрим-баги форком dsh: репортить/обходить
  компенсаторами, как сделано для D6.

## Журнал

- 2026-11-04: создано (`gitctl backlog new`). Контекст: тур D6
  (03.11, agctl e956694) показал, что rc.2 требует двух компенсаторов
  в chat-brand; апгрейд пина — путь их убрать.
- 2026-11-04: done — Пин обновлён bin/dsh-bump.sh --update (dist-tags.latest=rc.5), образ пересобран (sha 3d9c30f4f2f, dsh --version=0.2.1-rc.5). Контракт-аудит по исходникам тега: uiWorkspace.startSession/closeRightbar/developerTools preference-форма - идентичны rc.2-форме chat-brand 0.6.x. Верификация: bin/e2e-first-session.sh зелёный на rc.5 (первый запуск + reload), D7/D8 DOM-пробы на изолированном стенде - user Full access/кнопка скрыта, developer readyFlag=1+Workspace Write, defaultPreset в живом dsh-home. Примечание: bin/e2e-smoke.sh упал на легаси-create 410 (v3-надгробие) - смок не адаптирован к v…
