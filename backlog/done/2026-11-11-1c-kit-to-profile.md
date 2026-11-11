---
schema: 1.0.0
id: 1c-kit-to-profile
title: "Комплект «1С-разработка» — из ядра в профиль: убрать копию шаблона 1c-vibe и сверку check-1c-vibe-sync"
status: done
priority: P2
size: M
area: platform
repo: arijjia/agctl
created: 2026-11-11
updated: 2026-11-11
tags: [profile, kits, 1c-vibe, cleanup]
depends: []
epic: ""
---
## Описание

После переноса сценариев, каталога пакетов и надстройки образа в профиль
(ветка agent/profile-catalog) в ядре остался предметный комплект
«1С-разработка»: запись `1c-vibe` в `provision.Kits` и `provision.Templates`
и встроенная копия репо-донора `internal/assets/templates/1c-vibe`
(AGENTS.md, Python-тулзы Jira/GitLab/BSL, демо-проект, скилл). Копию
сторожит сверка `bin/check-1c-vibe-sync.sh` (`make tpl_check_sync`),
которую надо гонять перед каждым релизом бинаря.

Разбор показал, что комплекты раскладывает только legacy-джоба v2
`provisionJob`, у которой нет вызовов. Живой путь 1С — готовый сценарий
профиля `utmn` (`workspace.scenarios`), который клонирует то же репо
`arijjia/1c-vibe` напрямую. Значит, переносить копию шаблона в репо
профиля не нужно: это была бы вторая копия с тем же дрейфом. Достаточно
убрать комплект из ядра, а 1С оставить сценарию профиля.

## План

1. Убрать комплект `1c-vibe` из `provision.Kits`, его компоненты из
   `componentSources` и запись из `provision.Templates`; в ядре остаётся
   только универсальный `base`.
2. Удалить `internal/assets/templates/1c-vibe`, `bin/tpl.mk`,
   `bin/check-1c-vibe-sync.sh` и упоминания `tpl_check_sync` в доках.
3. Обновить тесты комплектов и ручек `/api/kits`, `/api/templates`.
4. Guard-тест: маркеры, которые ловят возврат предметного шаблона 1С в
   ядро.

## Критерии готовности

- В ядре нет предметных шаблонов организаций; `/api/kits` отдаёт только
  `base`.
- Сценарий 1С-vibe по-прежнему доступен в профиле `utmn`.
- `go vet ./... && go test ./...`, `make web_build` проходят; CI зелёный.

## Журнал

- 2026-11-11: сделано в ветке agent/ci-fix-1c-kit. Комплект `1c-vibe`
  убран из `provision.Kits`, `componentSources` и `provision.Templates`;
  удалены `internal/assets/templates/1c-vibe`, `bin/tpl.mk`,
  `bin/check-1c-vibe-sync.sh`. `/api/kits` и `/api/templates` отдают только
  `base`. 1С доступен сценарием профиля `utmn`. Guard ловит маркеры
  `1c-tooling` и `templates/1c-vibe`; тест `TestCoreKitsAreOrganizationFree`
  не даёт комплекту вернуться. Тесты, vet и `make web_build` зелёные.
