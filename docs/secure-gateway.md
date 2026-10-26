# Защищённый шлюз к DSH Web: модель v3

Описывает единую целевую схему v3 и текущие security boundaries, не является
сертификацией. У пользователя один персональный runtime независимо от роли;
`developer` добавляет SSH-канал в тот же контейнер, но не отдельное dev-место.

## Поток запроса

```text
Browser -- HTTPS --> trusted ingress -- HTTP --> agctl serve :8929
                                                     |
                                      /chat/ + cookie session check
                                                     |
                               agctl reverse proxy / boot-token mint
                                                     |
                      127.0.0.1:<dynamic host port> -> same user container dsh web :3080 (bind eth0, без релея)
                                                                     -> optional sshd :2222 (developer)
```

1. После входа пользователь открывает `/chat/`. Этот маршрут аутентифицируется
   сессией agctl и лениво обеспечивает runtime текущего пользователя; браузер не
   выбирает отдельный workspace. Путь корня после входа тоже ведёт в `/chat/`
   ([регистрация маршрутов](../internal/server/server.go#L306-L319),
   [проверка сессии](../internal/server/server.go#L541-L549),
   [ensure-on-request](../internal/server/chat.go#L499-L553)).
2. Один runtime и один постоянный storage ID соответствуют пользователю;
   `user`, `developer` и `admin` не создают параллельные chat/dev контейнеры.
   Idle TTL по умолчанию — 60 минут для всех; остановка удаляет контейнер, не
   данные на volume ([настройка `serve`](../cmd/agctl/main.go#L117-L129),
   [reaper](../internal/server/chat.go#L571-L599),
   [Docker volume и lifecycle](../internal/runtime/dockerrt/chat.go#L13-L79)).
3. Agctl проксирует `/chat/` в endpoint персонального runtime, срезает префикс,
   поддерживает streaming/WebSocket и запрещает cross-origin `Origin`. При
   upstream 401 boot token подставляется сервер-сервер; путь с токеном в
   браузер не передаётся ([chat proxy](../internal/server/chat.go#L660-L769),
   [серверный mint transport](../internal/server/gateway.go#L208-L236)).
4. Сессия agctl и DSH auth-cookie — разные credentials. При mint DSH выдаёт
   свою HttpOnly/SameSite=Strict cookie; перед upstream agctl удаляет `agctl_*`
   cookies, DSH-cookie ограничивается `/chat/`, а последующие запросы всё равно
   проходят проверку платформенной сессии ([очистка cookies](../internal/server/gateway.go#L255-L270),
   [scoping DSH-cookie](../internal/server/chat.go#L726-L754)).
5. DSH внутри контейнера слушает конкретный адрес eth0 (`dsh web --host <ip>`;
   апстрим dsh запрещает только wildcard-бинд, поэтому релей в образе не нужен
   и не живёт). Docker публикует web-порт только
   на `127.0.0.1` хоста; на macOS контейнерные процессы исполняются в Linux VM
   Docker Desktop, а не как отдельные процессы macOS
   ([выбор адреса и запуск](../internal/assets/image/entrypoint.sh),
   [bind-ip + healthcheck](../internal/assets/image/dsh-net.mjs),
   [host port bindings](../internal/runtime/dockerrt/docker.go#L269-L276)).

## TLS и запуск agctl

`agctl serve` по умолчанию слушает `127.0.0.1:8929`; это обычный HTTP listener,
он **не терминирует TLS**. Для сетевого пилота поставьте доверенный HTTPS
ingress/reverse proxy перед agctl, направьте UI, `/api/*` и `/chat/*` в один
origin, проксируйте WebSocket и streaming/SSE, не записывайте секреты в access-
логи и держите backend во внутренней сети. `--secure-cookies` выставляет атрибут
`Secure`, но сам HTTPS не включает
([serve flags](../cmd/agctl/main.go#L117-L129),
[HTTP listener](../cmd/agctl/main.go#L197-L213)). Не публикуйте незащищённый
backend на внешнем интерфейсе.

По умолчанию CORS выключен. Если отдельному клиенту действительно нужен
cross-origin доступ, `--cors-origin` принимает только точные origins с
credentials; wildcard не используется
([CORS allow-list](../internal/server/server.go#L321-L345)). Для обычного same-
origin интерфейса `/chat/` этот режим не нужен.

## SSH для developer в том же runtime

Роль `developer` запрашивается через `/api/role-requests`; после решения
администратора она назначается учётной записи. Роль даёт SSH-возможности в том
же личном runtime, не право создавать workspace seat
([обработка заявки и назначение роли](../internal/server/roles.go#L22-L50),
[апрув и привязка к аккаунту](../internal/server/roles.go#L79-L121)).

`GET`/`PUT /api/me/ssh` доступны только developer/admin. `PUT` сохраняет
публичный ключ; он применяется при следующем старте контейнера. Если у
developer нет ключа, sshd не запускается. При наличии ключа sshd работает внутри
личного контейнера на `:2222`, только key-auth; root password login, password
и forwarding отключены. Docker публикует порт на динамический `127.0.0.1` host
port; `sshPort` API равен 0, когда runtime спит. Для подключения с другой
машины требуется доверенный туннель/VPN, а не прямое подключение к контейнеру
([routes `GET`/`PUT /api/me/ssh`](../internal/server/server.go#L243-L251),
[role check и API](../internal/server/roles.go#L124-L184),
[key передаётся в общий runtime](../internal/server/chat.go#L536-L568),
[условный sshd и ограничения](../internal/assets/image/entrypoint.sh#L22-L45),
[публикация только loopback](../internal/runtime/dockerrt/docker.go#L273-L279)).

## Проекты, mounts и локальное сохранение

Контейнер получает два RW bind mounts: `$AGCTL_HOME/chats/u<ID>/dsh-home` в
`/home/dsh` (DSH_HOME) и `$AGCTL_HOME/chats/u<ID>/work` в `/workspace`.
DSH Web и его процессы-дети читают и пишут файлы напрямую в эти mounts;
операции с файлами не проходят через `agctl serve` и его HTTP API.

Проекты располагаются в общем user volume как
`chats/u<ID>/work/<name>` (в контейнере — `/workspace/<name>`). Это текущий
source snapshot, а не финальная миграция. Целевой вариант
`$AGCTL_HOME/u<ID>/{dsh-home,workspace/<project>}` обсуждается как раскладка
тома; в обоих вариантах `dsh-home` и `workspace` — отдельные каталоги-соседи,
а не workspace внутри DSH_HOME. Autosave использует локальный `go-git` в процессе agctl: commit остаётся внутри проекта,
в процессе agctl: autosave-коммит остаётся в проекте и не push-ится. Проект,
созданный с нуля, не имеет remote; GitHub-клон сохраняет свой `origin`. Это не
отдельный git-процесс, workspace или container ([project API](../internal/server/projects.go#L14-L50),
[путь и каталог проекта](../internal/provision/projects.go#L45-L65),
[локальный autosave](../internal/provision/projects.go#L210-L273),
[autosave goroutine внутри agctl](../internal/server/projects.go#L52-L72)).

## Legacy routes и локальный backend

- Целевой v3 UI использует `/chat/`; `/w/{id}` и модель workspace seats —
  legacy, не часть новой пользовательской навигации. Создание/изменение старых
  мест и все `/api/workspace-requests` endpoints отвечают `410 Gone`; чтение
  старых workspace-строк сохранено, но workspace-request API больше не читает
  старые заявки. Низкоуровневый `/w` proxy и его маршруты оставлены для
  legacy-записей и compat/tests:
  **текущий код всё ещё обслуживает старые `/w/{id}` записи через защищённый
  proxy — это не означает, что `/w` является целевым v3 UX**. Для legacy-записи
  `GET /w/{id}/manifest.webmanifest` остаётся публичным (статический manifest),
  остальные UI/API proxy routes требуют agctl session и проверку owner/admin
  ([manifest и `/w` registrations](../internal/server/server.go#L284-L293),
  [public manifest handler](../internal/server/gateway.go#L57-L70),
  [410 на создание места](../internal/server/handlers.go#L179-L181),
  [workspace-request tombstones](../internal/server/handlers.go#L488-L502),
  [compat proxy](../internal/server/gateway.go#L39-L55),
  [tombstone tests](../internal/server/approval_test.go#L8-L29)).
- Экспериментальный local-backend/connector код ещё существует, но владелица
  отложила это направление на осмысление. Это не часть целевого v3 UX.
  `--experimental-local-bridge` выключен по умолчанию; флаг включает отдельный
  pairing/transport путь, а не второй штатный режим для пользователя
  ([experimental flag](../cmd/agctl/main.go#L120-L139),
  [flag-gated routes](../internal/server/server.go#L256-L283)).

## Проверки и остаточные риски

- **SSH activity продлевает idle lease.** В последнюю минуту TTL Docker
  `ContainerTop` ищет дочерний `sshd` process и обновляет `lastSeen` для активной
  SSH/IDE-сессии. Если process inspection временно ошибается, reaper пропускает
  один тик, чтобы не оборвать работу. Detached-задачи после закрытия SSH не
  учитываются; это отдельный остаток.
- В образ добавлен healthcheck dsh web (HTTP на eth0-адресе контейнера,
  `agctl-dsh-net health`); reconcile останавливает
  и выселяет unhealthy personal runtime из кэша, после чего `/chat/` пересоздаёт
  его на сохранённом томе ([Docker HEALTHCHECK](../internal/assets/image/Dockerfile#L49-L55),
  [reconcile unhealthy chat](../internal/server/server.go#L459-L490)).
- **Boot token остаётся в Docker logs.** Agctl читает его оттуда для серверного
  mint; gateway не удаляет запись из Docker daemon. Доступ к Docker socket,
  daemon logs и каталогу данных сервера — привилегированный доступ; ограничьте
  его администраторами и не пересылайте логи без редактирования
  ([чтение логов runtime](../internal/runtime/dockerrt/chat.go#L81-L84),
  [извлечение boot token](../internal/server/server.go#L596-L635)).
- **Egress открыт / не ограничен gateway-политикой.** K8s prototype задаёт
  ingress-only NetworkPolicy на dsh web port и не ограничивает egress; она не
  gateway-specific, а target CNI не валидирован для production
  ([K8s policy prototype](../internal/runtime/k8srt/provider.go#L251-L279)).
- Host-порты привязаны к loopback, но не к отдельному Unix socket: другой
  локальный процесс/пользователь на том же хосте может обратиться к
  опубликованному порту. На macOS учитывайте границу Docker Desktop Linux VM и
  публикацию порта обратно на loopback хоста
  ([Docker port bindings](../internal/runtime/dockerrt/docker.go#L273-L279)).
- Для старого `/w/{id}` proxy остаётся различимость `403` (чужая запись) и
  `404` (несуществующая запись), то есть legacy gateway раскрывает факт
  существования старого ID авторизованному пользователю. Прямой доступ к
  legacy-строкам и proxy нужно удалить отдельной миграцией, если compat/tests
  больше не потребуются ([legacy owner check](../internal/server/gateway.go#L39-L55)).
- DSH-cookie остаётся доступна вкладкам того же workspace/origin; XSS внутри
  DSH остаётся угрозой. `Secure` защищает cookie только при HTTPS, реально
  завершаемом на доверенном ingress. Схема не является сертификатом ФСТЭК/152-ФЗ
  и не заменяет модель угроз и security review до работы с реальными данными.
