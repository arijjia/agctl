---
schema: 1.0.0
id: chat-activity-aware-ttl
title: "Activity-aware TTL: SSH/долгие процессы продлевают lease персонального контейнера"
status: done
priority: P1
size: M
area: runtime
repo: arijjia/agctl
created: 2026-10-25
updated: 2026-10-25
tags: []
depends: []
epic: v3-unified-runtime
---
## Описание

Сейчас idle-reaper учитывает HTTP/WS-активность шлюза `/chat/`, но developer может работать только через SSH/IDE. Чтобы часовое усыпление не оборвало открытую SSH-сессию, незадолго до TTL проверять process list личного Docker runtime и продлевать `lastSeen`, если есть дочерний `sshd`-процесс соединения. Не запускать дополнительный контейнерный процесс; process inspection — через Docker Engine API. Долгие команды внутри открытой SSH-сессии защищены тем же lease; detached-процессы после закрытия SSH-соединения в первой итерации не обнаруживаются.

## Критерии готовности

- Активная SSH-сессия в developer-runtime продлевает lease; после её закрытия неактивный runtime засыпает по обычному TTL.
- User runtime без sshd не делает дополнительных process-inspection запросов.
- Ошибка проверки Docker process list fail-closed на один reaper tick: активная работа не обрывается вслепую; событие логируется.
- Unit-тесты покрывают listener-only vs connected sshd child, активную SSH-сессию и последующее idle expiry.
- README/architecture описывают границу: detached background job после закрытия SSH в этой итерации не удерживает контейнер.

## План

- Уточнить Docker Engine client `ContainerTop` API и формат process rows.
- Добавить optional process-activity contract к ChatProvider; Docker ищет sshd child, K8s prototype возвращает no-op (SSHPort=0).
- В reaper проверять SSH только в последнюю минуту TTL, вне `chatMu`; при живой сессии обновить `lastSeen`.

## Не делать

- Не добавлять sidecar/daemon/heartbeat-процесс в пользовательский контейнер.
- Не считать idle по размеру тома или файловым mtime.
- Не обнаруживать detached jobs после закрытия SSH в этой итерации.

## Журнал

- 2026-10-25: создано (`gitctl backlog new`).
- 2026-10-25: Взято в работу: учесть активные sshd sessions при idle-reaper; выяснить стабильный Docker API signal без shell-скриптов внутри контейнера.
- 2026-10-25: Реализовано: перед TTL Docker ChatProvider.HasActiveSSH через ContainerTop (ps -eo pid,args) проверяет sshd child; live session обновляет lastSeen. Ошибка inspect fail-closed на тик. Detached jobs после закрытия SSH не отслеживаются. go test ./... и go vet ./... зелёные; Docker-live проверка в среде без рестарта рантайма не проводилась.
- 2026-10-25: done — Активная SSH-сессия продлевает TTL; detached-job detection оставлено отдельной задачей
- 2026-10-25: Финальная проверка: ContainerTop вызывается в последнюю минуту TTL только для SSH-enabled runtime; процесс sshd connection child обновляет lastSeen; stop после закрытия сессии проверен. go build/vet/test ./... зелёные, plugin tests 12/12. ContainerTop-live к уже запущенному runtime не проверялся без рестарта.
