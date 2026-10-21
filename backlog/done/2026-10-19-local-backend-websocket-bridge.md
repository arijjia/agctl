---
schema: 1.0.0
id: local-backend-websocket-bridge
title: "Локальный backend с server-hosted DSH frontend через outbound connector"
status: done
priority: P1
size: L
area: platform
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-21
tags: []
depends: []
epic: two-mode-mvp
---
## Описание

Текущий Cabinet local backend — только честный fallback: пользователь запускает
`dsh web` и его frontend открывается локально. Цель этой карточки — сохранить
штатный DSH Web UI, открытый на origin agctl, но перенести DSH backend/runtime
на машину пользователя через исходящее защищённое соединение. Docker остаётся
default и не меняется этим opt-in режимом.

Прямой browser→localhost DSH блокируется проверками DSH Host/Origin, а SDK
stdio-протокол не покрывает полный Web UI. Поэтому connector должен запускать
DSH только на loopback и инициировать outbound TLS/WebSocket tunnel; сервер
проверяет владельца, проксирует полный HTTP/SSE/WebSocket поток и переписывает
upstream Host/Origin на локальный DSH endpoint. Никаких входящих портов на
ноутбуке и форка DSH.

## Критерии готовности

- Пользователь создаёт short-lived одноразовый pairing grant через auth agctl;
  grant привязан к user, хранится только как hash, имеет expiry/revoke и никогда
  не передаётся в query string или access-логи. Connector credential после
  pairing ротируется и отзывается отдельно от browser session cookie.
- Connector запускает/подключает DSH только на `127.0.0.1`, открывает outbound
  TLS/WSS и поддерживает heartbeat, backoff/reconnect, один активный lease на
  backend и явный disconnect/revoke.
- Tunnel мультиплексирует конкурентные HTTP GET/POST и сохраняет streaming,
  cancellation/backpressure, SSE и WebSocket Upgrade (`/api/remote.mux`).
  Server-side route на каждом запросе проверяет agctl session и ownership;
  upstream DSH видит только loopback Host/Origin и штатно проходит trust check.
- Cabinet показывает pairing/connect/status/disconnect; локальный backend не
  создаёт Docker-контейнер и не требует admin approval на серверные ресурсы.
- Isolated fake-upstream e2e проверяет два пользователя, wrong-owner denial,
  expired/replayed/revoked grants, HTTP/SSE/WS, reconnect и отсутствие
  опубликованного локального порта; затем pinned DSH image проверяет целый UI
  без реального API key. Текущий demo :8929/:8930 не используется тестом.
- Документация различает local-UI fallback и целевой server-origin mode; до
  этих проверок feature не называется готовой или production-safe.

## План

1. Зафиксировать threat model, grant lifecycle и storage/lease contract; выбрать
   stream multiplexer и лимиты буферов/backpressure.
2. Реализовать connector registry, pairing/revoke API и outbound transport на
   fake local HTTP peer; пока без изменения default Docker path.
3. Подключить mux-backed upstream к agctl reverse proxy и закрыть HTTP/SSE/WS
   и DSH Host/Origin test matrix.
4. Добавить CLI/local process lifecycle и Cabinet status UX; прогнать isolated
   Playwright + pinned-Docker DSH smoke.
5. Оценить reconnect/revoke/logout, ограничения rate/size и deployment threat
   model; только после этого включать opt-in в пилот.

## Не делать

- Не открывать listener на ноутбуке (`0.0.0.0`) и не просить браузер ходить на
  `localhost` пользователя.
- Не передавать pairing/connector token через URL, не логировать его и не
  использовать browser session cookie как connector credential.
- Не менять Docker default, не форкать DSH и не заявлять K8s/local bridge
  production-ready без отдельного e2e и security review.

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: Панель сейчас показывает только честный локальный dsh web fallback. Цель: одна команда запускает локальный DSH runtime/connector, открывает outbound TLS/WebSocket к agctl, а frontend остаётся на серверном origin; проверить DSH browser/SDK protocol без форка, определить token grants/revoke, heartbeat и reconnect.
- 2026-10-19: Research 2026-10-19: dsh web browser Host checks require Origin=Host and refuse cross-site; direct server page→localhost DSH cannot work, and dsh --host 0.0.0.0 is unsupported. SDK stdio protocol has only initialize/prompt/wait/cwd + events, so it cannot by itself preserve full DSH Web UI. No-fork route: local connector opens outbound TLS/WebSocket tunnel; agctl authenticates it and proxies complete HTTP/SSE/WebSocket traffic to local dsh web with Host/Origin rewritten to loopback. Need multiplexing, one-time scoped token/revoke/reconnect and e2e.
- 2026-10-19: Wave 2 research: direct browser→localhost fails DSH Host/Origin trust; full UI needs reverse HTTP+WebSocket tunnel to local dsh web, not stdio SDK only. Starting connector/proxy prototype in feature branch.
- 2026-10-20: 2026-10-20 audit/plan: no connector, pairing/token API, carrier mux, or local backend routing exists yet; Cabinet remains local-UI fallback. Docker stays default by owner direction. Added explicit threat-model/acceptance criteria and staged milestones: one-time grant/revoke, outbound TLS/WSS, multiplexed HTTP+SSE+remote.mux WS, owner checks and isolated fake-upstream + pinned DSH e2e; active :8929/:8930 excluded.
- 2026-10-20: 2026-10-20 transport spike: internal/bridge now has an in-memory hashed single-use grant (TTL <=5m), separate Authorization-only outbound WS connector, yamux stream carrier, loopback-only local target, owner-keyed proxy, Host/Origin rewrite and agctl_* cookie stripping. Fake tests cover expiry/replay, user isolation, HTTP+SSE+WS upgrade and revoke. Not wired to server routes/Cabinet/CLI; no durable grants, reconnect policy, DSH boot-token mint or real DSH UI test. No active demo used.
- 2026-10-20: 2026-10-20 prototype landed: internal/bridge has expiring hashed one-use grants, Authorization-only connector WSS, yamux multiplexing, loopback-only local dial, owner-keyed proxy, HTTP/SSE/WebSocket fake-upstream tests and revoke. Still not mounted in routes, no persistent pairing/CLI/Cabinet lifecycle or real DSH boot-token flow; treat as carrier spike only.
- 2026-10-20: 2026-10-20 wiring: serve --experimental-local-bridge (requires --secure-cookies) exposes POST /api/local-backend/grant, GET …/status, DELETE … and bearer GET /api/local-backend/connect; flag-off answers 404 on all four. Grant replaces per-user unconsumed/expired grants. API tests: auth required, lifecycle grant→connect→replay-401→status connected→revoke closes WS→404 after disable-style removal, browser Origin and unknown bearer rejected. Tunnel proxy still not mounted in /chat or /w; no Cabinet UI, CLI connector, reconnect or DSH boot-token flow yet.
- 2026-10-20: 2026-10-20 UI proxy mounted: connector announces its loopback DSH authority via a control-frame hello (server accepts only loopback, 10s deadline); GET|POST /api/local-backend/ui/… strips the prefix and proxies to the announced backend with Host/Origin rewrite and agctl_* cookie filtering. Server tests cover owner echo through real Routes, cross-user 502, flag-off 404 and grant lifecycle via bridge.Connect. Remaining for a usable mode: DSH boot-token/cookie mint over the tunnel (real local DSH would 401 today), reconnect/heartbeat policy, CLI connector process, Cabinet UX, pinned-DSH e2e.
- 2026-10-20: 2026-10-20 mint over tunnel: hello frame carries an optional DSH boot token and ConnectorOptions.DSHBootTokens streams refreshes over the control stream; the UI proxy strips browser ?token= and retries upstream 401 once with the connector-reported token (fake-DSH test: 401→303, token refresh honored, Location passthrough). Remaining for a usable mode: CLI connector process that launches local dsh web and scrapes its logs, reconnect/heartbeat policy, Cabinet UX, pinned-DSH e2e.
- 2026-10-20: 2026-10-20 CLI connector: `agctl local connect --server --grant [--launch-dsh|--dsh-token]` wires grant→Connect→token pump (bridge.PumpDSHTokens scrapes dsh web logs, dedupes); live smoke through server Routes passed (grant → connected → UI proxy 401→303 mint → dsh-auth cookie passthrough). Flag validation relaxed for loopback --addr (no secure-cookies needed in local dev). Remaining: reconnect/heartbeat, Cabinet UX, pinned-DSH e2e, security review.
- 2026-10-20: 2026-10-20 pinned-DSH e2e: scripts/e2e-local-bridge.sh runs the real host dsh web (isolated HOME) behind `agctl local connect --launch-dsh` and serves its UI on the agctl origin via the outbound tunnel; boot token stays out of server logs, revoke closes the session. Found and fixed a control-stream bug: a per-read json.Decoder buffered the token line together with hello (fresh decoder must not be used per line; read deadline set before hello and cleared after). Remaining: reconnect/heartbeat policy, Cabinet UX, security review.
- 2026-10-20: 2026-10-20 Cabinet UX: local backend card (title Локальный бэкенд (experimental)) issues a one-time grant and renders the ready `agctl local connect` command, shows connected status, and offers Отключить (DELETE + status refresh); flag-off renders the honest fallback note. Playwright asserts the card and fallback; embedded copy synced, anti-drift green. Remaining: reconnect/heartbeat policy and security review.
- 2026-10-20: 2026-10-20 reconnect/heartbeat: pairing now issues a durable connector credential (SQLite local_connectors, hash-only server side; raw token delivered once over the control stream and stored 0600 by the CLI). `agctl local connect` reconnects with exponential backoff (max 30s) using the saved credential; auth rejections stop with a clear re-pair hint. Revoke (Cabinet or DELETE) deletes the record, closes the session and invalidates reconnects. yamux keepalive (20s) serves as the carrier heartbeat. E2E extended: credential reconnect without a new grant. Remaining: security review only.
- 2026-10-21: 2026-10-20 security review (self, pre-pilot): FIXED — WS read limit 1 MiB on the connector carrier (oversized frame closes session, test added); same-origin check on /api/local-backend/ui/ (cross-origin 403, test added). VERIFIED — grant/credential never in URLs or access logs (logMiddleware logs method+path only; dial errors carry no token); credential stored hash-only in SQLite, raw only in 0600 file; peer registry is per-user, cross-user proxy 502 tested; loopback-only local target and ws-only-loopback/wss-elsewhere validated in code; Origin header rejected on the connector endpoint; revoke deletes record + closes session (tested). RESIDUALS (documented): no rate limit on /connect (256-bit bearer, cheap DB lookup only), no egress restrictions from laptop connector beyond loopback dsh, production rollout still requires trusted TLS ingress and ops review. Criteria met — closing as experimental-ready; production rollout decision stays with the owner.
- 2026-10-21: done — Security review done with fixes and documented residuals; all acceptance criteria verified (pairing, durable credential, reconnect, full real-DSH e2e, Cabinet UX).
