---
schema: 1.1.0
id: developer-multi-containers
title: "Лаунчер: несколько параллельных контейнеров на одного developer (редкий кейс)"
status: done
priority: P3
size: L
area: server
repo: arijjia/agctl
created: 2026-11-04
updated: 2026-11-15
tags: ["developer", "launcher", "multi-runtime"]
depends: []
sessions:
  - session-4e3c026b-5dcd-4cb7-96f8-7b7c83085443:work
  - session-1247c577-a767-45f9-80c3-6e31877ad29a:work
---
## Описание

Владелец (2026-11-04): «возможно, будет возможность редким пользователям по
нескольку контейнеров запускать». v1 лаунчера (developer-launcher-images)
даёт один персональный runtime со сменой образа; эта карточка — про
настоящий мульти-рантайм: несколько живых контейнеров одного developer с
разными образами и общим или раздельным томом (решить), выбор активного в
UI. Затрагивает контракт ChatProvider (RuntimeID per (user,slot)), StorageID
(том per slot), TTL-рипер (несколько lease на юзера) и квоты контейнеров
(лимит slot'ов на роль).

## Критерии готовности

- [ ] дизайн-решение: том общий vs per-slot; лимит слотов по роли
- [ ] ChatProvider: EnsureChat(slot-spec), лаунчер видит все слоты
- [ ] TTL-рипер и админ-панель видят слоты как отдельные контейнеры

## Не делать

- Не ломать модель «один runtime на user» для роли user.

---
## Журнал

- 2026-11-04: создано (`gitctl backlog new`).
- 2026-11-15: Дизайн-решения (критерий 1, зафиксированы за владельцем как v1 с обоснованием): том PER-SLOT — слот 0 исторический u<ID>, слот N плоский u<N>s<N> (dockerrt запрещает слэши в StorageID; разные образы = разные toolchain'и, общий workspace двух живых агентов = гонки/git-конфликты; обмен файлами между слотами — через git); лимит живых слотов developer = 3 (--launcher-slots-max / AGCTL_LAUNCHER_SLOTS_MAX / serve.launcher_slots_max; живые = running, спящие compute не занимают); роль user — ровно один слот (0) без лаунчера, как раньше. SSH-панель и /api/me/runtime ведут в активный слот; проектные API шлюза читают workspace активного слота (ProjectDirectoryIn/ProjectWorkRootIn в provision).
- 2026-11-15: Реализация (критерии 2-3): кэш рантаймов ключуется (user, slot) — chatKey через chat.go/launcher/roles/reaper/prewarm/runtime_api/admin; RuntimeID слота N = chat-<hash>-u<N>-s<N> (слот 0 без суффикса — существующие установки не мигрируют); TTL-рипер/health-reconcile/свип дрейфа работают по слотам автоматически; админ-панель — каждый слот отдельной строкой (ChatContainerInfo.Slot из суффикса -s<N>), admin stop/start/restart адресуют точный слот. API: GET /api/me/slots, PUT/POST /api/me/slots/{slot}/image|start|stop|activate; /chat/ открывает активный слот (user — всегда 0). UI: таблица слотов с выбором образа, старт/стоп/активация, бейдж активного.
- 2026-11-15: refl: refl: слоты сделали прозрачными для адаптеров — контракт ChatProvider не менялся (RuntimeID/StorageID несут слот), docker/k8s/ax не тронуты; проверено live на docker (2 параллельных контейнера u2s1/u2s2, раздельные маунты work+dsh-home, activate → /chat/ 200, stop чистит compute). Хвосты v2: общий том на слоты (обмен через одну рабочую копию) — если эксплуатация попросит; per-slot SSH-панель (сейчас активный слот); prewarm для неосновных слотов (греется только слот 0).
- 2026-11-15: done — Карточка закрыта (v1 мульти-рантайма): слоты developer'а — параллельные контейнеры с разными образами и per-slot томами, лимит живых слотов 3 (настраивается), активный слот в /chat/ и проектных API, рипер/health/свип/админка видят слоты отдельно. Проверено: go vet+tests (включая slots_test: совместимость имён слота 0, lifecycle API, лимиты и роли) и live-docker прогон (2 слота, маунты, activate, stop). Пуш — решение владельца.
