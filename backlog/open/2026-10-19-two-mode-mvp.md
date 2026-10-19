---
schema: 1.0.0
id: two-mode-mvp
title: "Два режима agctl v2: личный DSH-чат и разработка через Кабинет"
status: in-progress
priority: P1
size: L
area: platform
repo: arijjia/agctl
created: 2026-10-19
updated: 2026-10-19
tags: ["two-mode", "night-shift"]
depends: []
---
## Описание

(описать при взятии в работу)

## Критерии готовности

- (сформулировать)

## Журнал

- 2026-10-19: создано (`gitctl backlog new`).
- 2026-10-19: Видение владельца 2026-10-19: только два режима. Режим 1: после входа сразу DeepSeek Harness на корневом домене; многопользовательский; хранение файлов/истории переживает остановку sandbox; песочницы поднимаются лениво и могут удаляться; Git не нужен. Режим 2: разработка через встроенный DSH-плагин «Кабинет», Git + отдельный контейнер на workspace; 2a frontend+backend на сервере с admin approval; 2b backend локально, frontend на сервере, бесплатно по серверным compute. Требование обновляемости: не форкать DSH, поддерживать upstream pin/bump.
- 2026-10-19: Волна 1 (текущая): per-user lazy chat runtime + постоянный per-user DSH_HOME; root login → /chat; request/approve UI для серверных dev-workspaces; browser e2e. Волна 2: sessionId-scoped artifacts/dirs и resource quotas. Волна 3: outbound local connector/bridge, если DSH client API позволяет без форка; иначе зафиксировать проверенную техническую границу и UX-compatible fallback.
- 2026-10-19: Волна 1 проверена: per-user lazy chat/runtime и admin approval реализованы в feature branch agent/full-mvp-two-modes; go vet + go test -race, Playwright root→/chat→Cabinet→approve, Docker e2e 11/11; локальный bridge/sessionId-scoped files остаются следующими волнами.
- 2026-10-19: Wave 1 implementation currently on feature branch agent/full-mvp-two-modes (not pushed): Go race suite PASS; Docker per-user chat e2e PASS=11/11; Playwright root→chat→Cabinet→request→approve PASS; SSH e2e PASS=7/7 (real Mutagen round-trip verified 9/9 in a separate run). No branch merge yet. Remaining: sessionId-scoped working directories and outbound local connector bridge.
- 2026-10-19: R1.3 session-scoped cwd is now implemented as a hidden directory per DSH Session (e2e verified); new sessions remain in DSH Ungrouped because rc2 rejects workspaceId+cwd. Open follow-ups: group naming/presentation, quotas/retention/prewarm, local backend bridge.
- 2026-10-19: R1.3 session files now implemented and verified: DSH session.create is scoped to hidden /workspace/.sessions/<sessionId>; e2e-cabinet PASS=12/12; Go race PASS; Playwright root→chat→approval PASS; SSH+real Mutagen PASS=9/9. Remaining R1.3 quotas/prewarm and session-list Ungrouped polish; R2.2 local server-front bridge remains open.
- 2026-10-19: R1.3 completed: sessionController.create wrapper assigns hidden cwd /workspace/.sessions/<sessionId> before Session header persistence; remains within one per-user work volume and DSH Session history. e2e-cabinet PASS=12/12; Browser approval flow PASS; go test -race PASS. DSH UI currently presents these sessions under Ungrouped because rc2 forbids workspaceId+cwd.
- 2026-10-19: Владелец уточнил target: Kubernetes-native. Audit: per-user lazy lifecycle совместим как Pod+Service+PVC (delete Pod, retain PVC), ChatProvider уже выделен с StorageID/Endpoint и Docker adapter. Но текущий default constructor запускает только Docker; dev RuntimeIface хранит HostPort/host paths, control-plane SQLite+in-memory locks — не HA. Не называть текущую сборку K8s-ready до adapter/controller/PVC/lease и cluster e2e.
