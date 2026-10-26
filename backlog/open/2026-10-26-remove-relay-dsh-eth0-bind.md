---
schema: 1.0.0
id: remove-relay-dsh-eth0-bind
title: "Убрать релей: entrypoint dsh --host <eth0-ip> + healthcheck по /run/dsh-bind-ip, bump образа"
status: open
priority: P2
size: M
area: infra
repo: arijjia/agctl
created: 2026-10-26
updated: 2026-10-26
tags: ["perf", "relay", "dsh", "image"]
depends: []
---
## Описание

Резерч (agctl-hq `docs/v3/relay-removal-research.md`, 2026-10-26): релей
(`internal/assets/image/relay.mjs`) — чистая L4-труба `0.0.0.0:3080 → 127.0.0.1:3099`
без auth/правки заголовков/TLS, но это отдельный node-процесс на каждого активного
пользователя: **замер на живом u1 — VmRSS 81 MB** (dsh — 205 MB).

Запрет «dsh слушает только loopback» на деле запрещает только wildcard: схема
`dsh-host-webserver` отвергает лишь адрес из одних нулей и прямо предлагает
«bind one concrete IPv4 or IPv6 address of a local interface»; флаг
`dsh web --host <ip>` существует и задокументирован в пинованном 0.2.1-rc.2.
Бинд на конкретный eth0-IP контейнера выводит dsh наружу через обычный DNAT
публикуемого порта — релей не нужен.

Подтверждено живым экспериментом (образ `agctl-dsh:0.2.1`, docker 29.9.2):
`dsh web --host 172.17.0.4 --port 3080` + `-p 127.0.0.1:31987:3080` —
`GET /` → 401 через DNAT; фенс `/api` проходит по loopback-ветке
(шлюз agctl и так шлёт `Host: 127.0.0.1:<hostport>`, `internal/server/chat.go`
Rewrite) и по ветке бинд-адреса; чужой Host → 403; WS-проба неотличима от
прод-пути; boot-токен скрейпится регэкспом `[?&]token=…` без привязки к URL.
Шлюз, mint-транспорт, dockerrt, k8srt (NetworkPolicy на 3080) — без изменений.

Эффект: минус один node-процесс и ~81 MB RSS на активного пользователя
(20 активных ≈ минус 1.6 GB), быстрее старт контейнера, healthcheck начинает
говорить с dsh напрямую (упрощает канарейку из `2026-10-25-container-healthcheck-canary`).

## План

1. `internal/assets/image/entrypoint.sh`: убрать запуск релея; вычислить IP
   (node-однострочник по `os.networkInterfaces()`: не-internal IPv4, предпочтительно
   eth0; в k8s предпочесть `POD_IP` из downward API); записать в `/run/dsh-bind-ip`;
   `exec dsh web --host "$IP" --port "${DSH_PORT:-3080}" --no-open "$@"`.
2. `internal/assets/image/Dockerfile`: убрать `COPY relay.mjs` + chmod; healthcheck
   (сейчас `http://127.0.0.1:3099/` → станет REFUSED) перевести на чтение
   `/run/dsh-bind-ip` и запрос `http://$IP:3080/`; bump тега образа в `bin/img.mk`.
3. Прогнать E2E: чат открывается, `/chat/` и WS работают, логи контейнера без
   `[agctl-relay]`, healthcheck зелёный, reconcile не пересоздаёт место.
4. Документацию (README, `docs/architecture.md`, agctl-hq `docs/v3/*`) — пометить
   релей как удалённый одним точечным проходом.

## Критерии готовности

- В образе нет `relay.mjs` и в живом контейнере нет процесса `agctl-relay`.
- Единственный слушатель — `dsh web` на eth0-IP контейнера; `127.0.0.1` внутри
  контейнера dsh не отдаёт (healthcheck и все внутренние проверки переведены).
- Чат-путь сквозь шлюз работает без правок Go-кода: прокси, mint `?token=`, WS.
- RSS пользовательского контейнера ниже на ~80 MB (сверить `ps` до/после).
- Откат: один revert коммита + пересборка образа.

## Не делать

- Не трогать режим `agctl local connect --launch-dsh` (локальный dsh на ноуте —
  другой путь, релея там нет).
- Не менять порт 3080 насквозь (публикация dockerrt, NetworkPolicy k8srt, EXPOSE).
- Не добавлять флаг-переключатель «релей/direct»: одно состояние, откат — revert.

## Журнал

- 2026-10-26: создано (`gitctl backlog new`).
- 2026-10-26: тело карточки из резерча — вердикт «можно убрать», эксперимент и
  план; резерч-док: agctl-hq `docs/v3/relay-removal-research.md` (21e01f2).
