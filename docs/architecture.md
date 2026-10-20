# agctl v2 — архитектура: два режима платформы

> Актуально на 2026-10-19 (утро). Решение владельца: **только два режима**
> вместо трёх тиров; требования зафиксированы дословно ниже. Стратегическая
> рамка — `agctl-hq/docs/agctl-platform-concept-v2.md` (правка «два режима»
> заменяет прежние Tier 0/1/2 как продуктовую ось); теория сред —
> `agctl-hq/docs/agctl-sandbox-and-runtime-theory.md`. Предыдущая версия
> этого файла (три тира) — в истории git.

## 0. Требования владельца (дословно, 2026-10-19)

> **Режим 1.** «персистентная, бесконечная песочница, точнее персистентная
> будет именно хранение файлов, привязанных к сессиям, а песочницы будут
> убиваемая, поднялась и опустилась, когда надо… какая-то ленивая песочница…
> чтобы это был интерфейс дипсик Харнеса… он должен быть многопользовательским.
> Режим номер один выглядит как сразу на адресе как чат ГПТ запущенный дипсик
> харнесс многопользовательский с песочницей… нужно провести качественный
> резерч… может быть нужно будет вообще переписывать весь бэкэнд…
> наверное, что-то по типу один контейнер, запущенный на всех, ну или
> несколько контейнеров».
>
> **Режим 2.** «режим для разработки, опциональный… гит нам нужен будет
> именно для режима два, который будет доступен как плагин, встроенный в
> дипсик харнесс. В этом плагине можно настроить себе воркспейс и Workspace
> создать… два своих режима: первый — бэк и фронт оба на сервере…
> нужно получить права доступа, апрув от администратора; второй — бэк
> локально запущен, а фронт на сервере — бесплатный: хотите пользуйтесь…
> нужен отдельный докер-контейнер, два гит-репозитория. Нужно хорошо
> продумать пользовательский сценарий… насколько разработчик погружён
> в петлю… чтобы ему было комфортно… желательно, чтобы была возможность
> открыть в IDE, например в VS Code».
>
> **Пожелание.** «если будем что-то переписывать в dsh — хотим иметь
> возможность накатывать новые версии, которые у него появляются».

| | Режим 1 «Чат» | Режим 2 «Разработка» |
|---|---|---|
| Для кого | все (студенты, преподаватели, сотрудники) | разработчики (опционально) |
| Вход | корень платформы после логина | панель «Кабинет» внутри dsh (плагин) |
| Движок | dsh harness (интерфейс как ChatGPT) | dsh harness |
| Git | **не нужен** | обязателен (bare + клон на место) |
| Среда | ленивая убиваемая песочница; персистентны файлы сессий | постоянный контейнер + SSH/Mutagen-каналы |
| Изоляция | среда на пользователя (см. §1) | контейнер на место (уже есть) |

## 1. Режим 1 — многопользовательский dsh-чат с ленивой песочницей

### 1.1 Резерч-вердикт: можно ли без переписывания бэкенда dsh

Факты (по установленному `@deepseek-ai/dsh` 0.2.1-alpha.2, проверено 2026-10-19):

- **dsh web однопользовательский по построению**: один `DSH_HOME` = один
  владелец; аутентификация — boot-токен → cookie `dsh-auth-*`; сессии чатов
  лежат в общем `DSH_HOME` без разделения по пользователям; RBAC нет.
- «Account» в dsh (`dsh-api-account-controller`, `ui-settings-account`) — это
  **логин в DeepSeek Platform** (кредиты/бонусы/PKCE), а не учётные записи
  пользователей платформы. Мультиюзерности он не добавляет.
- Профили (`web/tui/headless/sdk/acp`) — тоже single-user.

Вывод: **многопользовательский режим внутри одного процесса dsh web без
форка невозможен**. Варианты:

| Вариант | Вердикт |
|---|---|
| Форк/переписывание бэкенда dsh (multi-user в одном процессе) | ❌ против: конфликт с требованием обновляемости (§3), месяцы работы, потеря апстрим-обновлений |
| Один общий контейнер dsh на всех | ⚠️ только демо-MVP: сессии/файлы/credentials общие → нет изоляции пользователей; для прода не годится |
| **Ленивый контейнер dsh web на пользователя** (гипотеза владельца «несколько контейнеров») | ✅ выбираем: без изменения dsh, изоляция по построению, обновляемость сохраняется |

### 1.2 Выбранная архитектура режима 1

- **Персистентность = том, песочница = контейнер** (формула владельца):
  - `chats/u<userID>/dsh-home/` и `chats/u<userID>/work/` — персональные тома;
    остаются на диске после остановки/удаления контейнера.
  - Контейнер dsh web — **убиваемая песочница**: создаётся при первом
    обращении к `/chat/`, останавливается reaper-ом после 15 минут idle
    (флаг `--chat-ttl-min`), затем пересоздаётся при следующем входе.
- **Изоляция**: контейнер и DSH_HOME отдельны на пользователя; случайный
  Docker container name включает hash `$AGCTL_HOME` + user ID, поэтому разные
  развёртывания на одном Docker daemon не конфликтуют.
- **Файлы per-session**: Host-плагин оборачивает создание новой DSH-сессии и
  назначает ей `cwd=/workspace/.sessions/<sessionId>` до сохранения Session;
  папка и header cwd живут на per-user work volume и восстанавливаются после
  сна контейнера. e2e создаёт сессию через публичный `/chat/api/session/create`
  и проверяет реальный путь.
- **Граница безопасности**: разные пользователи разделены контейнерами и
  томами. Папки разных сессий одного пользователя — организация/привязка
  артефактов, не security boundary: его shell может перейти в соседнюю папку.
- **LLM-доступ**: провайдер чата выбирается по приоритету: личный секрет
  пользователя (сохранён при создании dev-места или позже через кабинет) →
  платформенный по умолчанию (флаги/env `--llm-base-url/--llm-models/
  --llm-api-key`, напр. корпоративный LiteLLM с моделями qwens) → если ничего
  не задано, dsh показывает свой модал «Add an API key». Ключ платформы
  пишется только в DSH_HOME конкретного пользователя (его контур доверия) и
  в лог не попадает.
- **Шлюз**: маршрут `/chat/` (уже реализован) с cookie-mint; в per-user
  варианте резолвится в контейнер текущего пользователя.
- **Плотность** (замер 2026-10-18, M4/24GB): idle ~125 МБ/контейнер,
  warm start ~2 с → 1000 пользователей при 5–10 % одновременно активных =
  50–100 живых контейнеров ≈ 6–12 ГБ RAM + тома на диске. Узкое место —
  модельный контур, не контейнеры (подтверждено `capacity-bench*.sh`).

### 1.3 Этапность режима 1

| Шаг | Что | Оценка |
|---|---|---|
| R1.1 ✅ | Корень после login → `/chat/`; без cookie → `/app/`; default dsh UI + Cabinet plugin | done |
| R1.2 ✅ | Per-user lazy dsh runtimes, persistent user DSH_HOME/work volumes, ensure-on-request, 15-min TTL reaper, shutdown cleanup | done; `/chat/` больше не общий контейнер |
| R1.3 partial ✅ | Hidden per-session cwd + internal Workspace and flat Chat session list implemented; quotas/retention/prewarm remain | **M** |

### 1.4 Что видит пользователь: один домен, разные runtime

**Домен — общая точка входа, не один общий backend и не контейнер в браузере.**
После входа браузер остаётся на одном домене платформы; agctl по сессии
пользователя маршрутизирует пути:

```text
GET /             → без сессии: /app/ (вход); с сессией: /chat/ (dsh UI)
/chat/*           → dsh runtime режима 1 (цель: личный контейнер текущего пользователя)
/w/<workspaceId>/*→ контейнер выбранного dev-workspace, с проверкой owner/admin
/api/*            → control-plane agctl (пользователи, места, права, аудит)
```

Пользователь **видит интерфейс DSH в браузере**, а не контейнер. Один и тот же
код/версия dsh-клиента показывается всем, но запросы `/chat/*` проксируются к
личному backend-runtime, выбранному по auth-cookie; `/w/<id>/` — к конкретному
workspace. Сейчас dsh web сам раздаёт frontend bundle из runtime-процесса,
поэтому это «один UX/домен и одинаковые assets», а не обязательно один
физический процесс frontend на сервере. Позже можно отделить и кешировать
статический shell, не меняя маршрутизацию API.

Несколько dev-workspaces одного пользователя — отдельные URL на том же домене
(`/w/<id>/`): можно держать их в разных вкладках, переключать из «Кабинета»;
дефолтный чат `/chat/` остаётся точкой возврата. Никаких контейнерных IP
пользователь не видит.

### 1.5 Что «умирает» и что остаётся

Контейнер — это **процессы и временная ОС-среда**, не хранилище истины:

```text
Контейнер/DSH runtime: dsh web, процессы, tmp, установленные временно пакеты
                         ↑ attach на время активности; stop/remove безопасен
Персистентный volume:   DSH_HOME + файлы/артефакты пользователя
                         хранится на диске/PV/S3 независимо от контейнера
```

В режиме 1 per-user `work` volume смонтирован как `/workspace`. При вызове
`session.create` Host-плагин присваивает новому Session header `cwd` в
`/workspace/.sessions/<sessionId>` до создания Agent. Файлы и вложения, которые
агент сохраняет относительно текущего cwd, переживают удаление контейнера;
новая сессия получает новый пустой каталог. Все сессии остаются в одном DSH
runtime и видны в истории. DSH 0.2.0-rc.2 не принимает одновременно
`workspaceId` и индивидуальный `cwd`, поэтому Host-плагин создаёт внутренний
Workspace с путём этой сессии и передаёт только его id. Клиентский chat-brand
скрывает строки Workspace и показывает сессии плоским списком; пользователи
не видят файловых директорий. Это всё равно только файловая привязка, не
security boundary: shell того же пользователя может перейти в соседнюю папку.

### 1.6 Почему lazy-контейнер, а не только файловые ACL

| Модель | Сильная сторона | Ограничение | Где использовать |
|---|---|---|---|
| ACL/папки в одном обычном серверном процессе | минимальный расход, как DB/chat backend | dsh tools/shell могут исполняться с одним OS identity; промпт-инъекция или ошибка path-check открывает чужие файлы | данные чата/RAG без произвольного shell |
| Browser WASM (Pyodide-класс) | per-browser песочница, почти ноль server RAM | Python-подмножество, нет shell/системных бинарей; IndexedDB привязан к браузеру и может очищаться | простые преобразования, не полный DSH |
| Эфемерный `dsh headless` на задачу + session volume | ~200 МБ/~0,5 с на задачу по нашему замеру; runtime можно уничтожить | без собственного UI dsh web; нужен общий frontend/orchestrator, queue, файловый API и возврат артефактов | следующий шаг для очень высокой плотности Tier 0 |
| **dsh web container на пользователя, scale-to-zero** | полный штатный DSH UI/tools; kernel/FS/credentials разделены между пользователями | ~125 МБ на активный container; нужны TTL, volume, quotas и scheduler | **рекомендуемый production path режима 1** |
| Один DSH container на всех | простейший прототип | общие DSH-сессии, DSH_HOME, процессы, credentials и filesystem — нет multi-user security boundary | только локальное демо/доверенная группа, не production |

«Простая песочница, поднялась/умерла» — технически возможна, но надо
различать два решения: (a) browser WASM (среда прямо в браузере пользователя,
не DSH shell); (b) server task sandbox, где shared chat frontend вызывает
`dsh headless` job и монтирует session store. Чтобы сохранить **штатный DSH web
интерфейс** и одновременно не форкать dsh, самый прямой путь — lazy-container
на пользователя; task-level sandbox остаётся оптимизацией R1.2/R1.3, а не
условием первого релиза.

### 1.7 Как это отличается от Open WebUI

- **Open WebUI multi-user**: один web service обслуживает пользователей;
  чаты, роли, группы, knowledge/file metadata хранятся с application-level
  ownership/permissions. Это не означает OS-container на каждого — для обычного
  чата backend не исполняет произвольный shell пользователя.
- Встроенный Code Interpreter может работать через **Pyodide в браузере**:
  по документации Open WebUI это безопасная WASM-среда с ограничениями (Python,
  без shell); filesystem persistence на reload зависит от IndexedDB-настройки
  и привязана к browser profile. Для полного shell они отделяют **Open Terminal**.
- Open WebUI отдельно предупреждает: один shared Open Terminal container не
  является границей изоляции — общие shell/filesystem/network; для недоверенных
  multi-user случаев рекомендуются per-user containers через их Terminals /
  Kubernetes backend.

Отсюда «как ChatGPT/Open WebUI» не означает «один контейнер dsh на всех»:
можно иметь общий web shell/control plane и общий model API, но исполнительный
runtime обязан быть per-user либо per-task. Источники upstream:
[Open WebUI scaling](https://github.com/open-webui/docs/blob/main/docs/getting-started/advanced-topics/scaling.md),
[Code Execution/Pyodide](https://github.com/open-webui/docs/blob/main/docs/features/chat-conversations/chat-features/code-execution/index.md),
[Open Terminal multi-user warning](https://github.com/open-webui/docs/blob/main/docs/features/open-terminal/advanced/multi-user.md).

### 1.8 Несколько режимов и контейнеров на одном домене

Пользовательский маршрут не переопределяется выбором backend. Sidebar «Кабинет»
создаёт dev-workspace; agctl возвращает id/URL, и plugin открывает `/w/<id>/`.
Владелец может иметь одновременно `/chat/`, `/w/A/`, `/w/B/` — это разные
браузерные вкладки/sessions за одним origin, а не один контейнер или один
workspace. Корневой `/` всегда возвращает в чат. Серверный workspace (2a)
появляется в списке только после admin approval; локальный backend (2b) не
требует ресурсов сервера и не апрувится, но настоящий «server frontend → local
DSH backend» потребует outbound bridge (см. §2.2).

## 2. Режим 2 — разработка (плагин «Кабинет» в dsh)

Вход — панель «Кабинет» в сайдбаре dsh (уже сеется в каждый dsh-home из
скелета; создание мест, LLM-провайдер, SSH-ключ — работает). Git и контейнер
на место — как сегодня (bare + клон + контейнер, autosave). Два подрежима:

### 2.1 Подрежим «бэк и фронт на сервере» — с апрувом администратора

- Реализовано: флаг `--require-approval` включает заявку (шифрованный provider
  payload); пользователь видит статус, admin видит очередь в plugin «Кабинет»
  и может approve/reject; approve запускает provision. Квота проверяется при
  отправке и повторно при approve.
- Для локального ноутбучного `make demo` флаг выключен; для серверного
  deployment включать после настройки TLS, quotas и admin role.

### 2.2 Подрежим «бэк локально, фронт на сервере» — бесплатный, без апрува

Ресурсы пользователя → платформе не нужен серверный контейнер, поэтому
админ-апрув на вычислительные ресурсы не требуется. Остаются обычная
аутентификация платформы и квоты на shared model API.

**Важное уточнение топологии:** запустить `dsh web` локально — это просто,
но тогда локально будет и его frontend. Это полезный workaround, однако
**не выполняет требование «front на сервере, backend локально»**. Чтобы
сохранить центральный dsh UI и локально исполнять задачи, нужен мост:

### Проверенные ограничения DSH и мост без форка

По DSH `0.2.1-alpha.2` Client connection package и `0.2.0-rc.2` image contract:

- dsh web — это Host + web UI; browser Remote-unary использует HTTP, события —
  `/api/remote.mux` WebSocket. Browser Host trust сверяет `Host` и `Origin`
  и блокирует `sec-fetch-site: cross-site`; прямой запрос страницы платформы
  на `http://localhost:<порт>` локального DSH не станет рабочим решением.
- SDK profile — отдельный stdio JSON-RPC runtime с узким набором методов
  (`initialize`, prompt/wait, cwd и события), не полный wire-контракт DSH web UI.
  Перейти на SDK и сохранить целый штатный интерфейс без собственной UI-работы
  нельзя.
- Источники upstream:
  [browser connection/trust](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/client/connection/README.md),
  [public URL/trusted host](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/public-deployments.md),
  [SDK protocol](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/sdk/protocol/README.md).

**Рекомендуемый прототип без форка** — local connector запускает полный `dsh web`
на loopback и инициирует исходящий TLS/WebSocket control-tunnel к agctl;
платформа выдаёт одноразовый scoped token, проверяет user/session, затем
проксирует HTTP + SSE/WebSocket через tunnel к локальному DSH. Upstream к DSH
gateway получает переписанные `Host`/`Origin` локального listener; браузер
видит только origin платформы, на ноутбуке нет входящего порта. Контейнер и
серверный compute не нужны; LLM/API расход остаётся. Это proxy-carrier,
не переписывание DSH backend. Нужно отдельно проверить multiplexing, upgrade,
reconnect, revoke и обратную совместимость после `dsh-bump`.

**Текущий кодовый статус:** `internal/bridge` — transport spike: in-memory
single-use grant, outbound WebSocket + yamux, control-frame «hello» с
loopback authority и boot-токеном локального DSH (потом — обновления токена),
owner-keyed proxy с HTTP/SSE/WS, фильтрацией `agctl_*` cookies и mint-retry
401 сервер↔локальный DSH. Serve с `--experimental-local-bridge` (только
вместе с `--secure-cookies`) открывает pairing API `POST /api/local-backend/grant`,
`GET …/status`, `DELETE …`, bearer handshake `GET /api/local-backend/connect`
и UI-proxy `GET|POST /api/local-backend/ui/…` (флаг-off даёт 404 на всех).
Осталось для рабочего режима: reconnect/heartbeat политика, Cabinet UX вместо
curl, pinned-DSH e2e и security review. CLI-коннектор уже есть: `agctl local
connect` (--server/--grant, опционально --launch-dsh со скрейпом boot-токена
из логов или --dsh-token), флаг сервера требует `--secure-cookies` за TLS,
кроме loopback-адреса для локальной разработки. Это experimental контур,
не local mode.

Если «frontend на сервере» означает, что сами static assets обязаны
физически отдаваться сервером (не через proxied local dsh web), потребуется
ещё decouple DSH client bundle и подключить его к тому же carrier — отдельная
последующая волна.

Это **не готовая возможность DSH**, а отдельная задача **L** с threat model и
e2e. Нынешний fallback в панели запускает локальный `dsh web`, поэтому UI
тоже локальный; он не выдаётся за целевой server-origin режим.

### 2.3 UX-петля разработчика (подрежим «на сервере»)

Цель владельца: «плотно присутствовать в петле, комфортно, кодит в своём
режиме, открыть в IDE (VS Code)». Петля (реализована ночью 2026-10-18/19):

1. Создал место (с SSH-ключом) в панели «Кабинет» → `sshPort` виден там же.
2. **VS Code Remote-SSH** / JetBrains Gateway → воркспейс как локальный
   проект: та же ФС, что у агента → изменения агента видны мгновенно.
3. **`agctl sync`** (Mutagen two-way-safe) → локальная папка-зеркало для
   оффлайна; `.git` — источник истины, ревью дифами, откат ревёртами.
4. Терминал: `agctl connect --port <sshPort>` (sshd key-only, loopback; для
   удалённой машины — админский туннель, задокументирован).
5. Autosave: агент коммитит сам, agctl доставляет в bare каждые 2 мин.

Дозакрыть: встроить эти шаги в панель плагина («как подключить IDE» с
готовыми командами) — **S**.

## 3. Обновляемость dsh (пожелание владельца)

- **Не форкаем ядро dsh.** Все наши поверхности — снаружи: клиент-плагин
  «Кабинет» (Lazy-CJS-бандл, не требует сборки dsh), шлюз/обёртка agctl,
  минимальный entrypoint контейнера.
- Версия dsh пинится в Dockerfile (`@deepseek-ai/dsh@0.2.0-rc.2`);
  `scripts/dsh-bump.sh` обновляет пин и прогоняет e2e. Регламент: любые
  доработки поведения dsh — плагинами/профилями, патчи ядра не носим.

## 4. Сводный план и оценки

| Блок | Оценка | Статус |
|---|---|---|
| R1.1 ✅ | Корень после login → `/chat/`; без cookie → `/app/`; default dsh UI + Cabinet plugin | done |
| R1.2 ✅ | Per-user lazy dsh runtimes, per-user DSH_HOME/work volumes, ensure-on-request, TTL reaper, active-stream guard, graceful shutdown | done |
| R1.3 partial ✅ | Hidden per-session cwd + internal Workspace and flat Chat session list implemented; quotas/retention/prewarm remain | **M** |
| R2.1 ✅ | Заявка на server workspace + admin approve/reject (`--require-approval`), browser flow root→chat→request→approve | done |
| R2.2 fallback | Команда запуска dsh локально (frontend тоже локальный; fallback, не целевой server-front режим) | prototype |
| R2.2 target | Connector + outbound WebSocket bridge: server frontend, local DSH backend | **L** |
| R2.3 ✅ | SSH key input + connect/sync/IDE hints в панели | prototype done; CI/polish follow-up |
| Регламент обновления dsh (bump+e2e) | S | ✅ тулинг есть |

Проверки: `scripts/e2e-cabinet.sh` (две per-user сессии), `scripts/e2e-ssh.sh`,
`scripts/e2e-cabinet-playwright.sh` (root → chat → approval UI), `go test -race ./...`.

Оценка ресурсов при разном числе пользователей — `docs/capacity.md`.

## 5. Kubernetes compatibility: target vs current implementation

**Вердикт:** per-user lazy DSH runtime совместим с Kubernetes как модель,
но текущий продукт ещё не K8s-native. Chat API/gateway отделён контрактом
`ChatProvider`: `ChatSpec` несёт стабильные `RuntimeID`/`StorageID`, а
`ChatHandle.Endpoint` — внутренний HTTP origin. Gateway проксирует на `Endpoint`,
не знает контейнерный ID/порт.

Добавлен prototype `internal/runtime/k8srt`: он создаёт Pod + ClusterIP
Service + per-user PVC; PVC/Secret имена следуют стабильному `StorageID`, а
Pod/Service — replaceable `RuntimeID`. Он задаёт `RuntimeClassName`,
requests/limits, non-root, seccomp и отключает service-account token; idle stop
удаляет Pod/Service/NetworkPolicy, оставляя PVC/Secret. Init-container выполняет
`agctl init-chat-home`. Fake-client tests проверяют resource shape, StorageID
reuse и сохранение PVC. Но policy пока разрешает ingress любому Pod namespace
на relay-порт, egress открыт и CNI не проверен; pod readiness не ждётся.
`server.New` по-прежнему выбирает Docker; `cmd serve` не выбирает K8s provider,
а live-cluster smoke не запускался. Поэтому это не готовый K8s deployment.

### Целевой K8s lifecycle режима «Чат»

1. Один Pod с `dsh web` на активного пользователя (или Sandbox CR, владеющий
   этим Pod). Agctl выдаёт/находит runtime по `userID` и проксирует `/chat/` на
   **ClusterIP Service/DNS**. K8s prototype возвращает endpoint сразу после
   создания Pod; до readiness клиент увидит страницу прогрева/retry. Никаких
   `hostPort`, node IP или прямых портов в браузере.
2. Постоянный PVC на пользователя (`StorageID`), с подкаталогами DSH_HOME,
   workspace и `.sessions/<sessionId>`. Idle TTL удаляет/останавливает **Pod**,
   сохраняя PVC; следующий запрос пересоздаёт compute и монтирует тот же claim.
   Удаление PVC — отдельная retention/deletion операция, не действие reaper.
3. Образ DSH должен быть опубликован в registry, доступном кластеру, и закреплён
   digest/tag; `RuntimeClassName` (`gvisor`/`kata`) — опциональный режим после
   установки и проверки handler на узлах. K8s prototype уже задаёт
   requests/limits, non-root, seccomp, drop capabilities, выключает automount
   service-account token и ограничивает ingress NetworkPolicy (same-namespace,
   только relay-порт); egress остаётся открытым под user-configured LLM base
   URLs — residual. API key создаётся как K8s Secret, но init записывает его и
   в DSH_HOME PVC; нужны encryption-at-rest для Secret и PVC и RBAC smoke, без
   этого deployment нельзя считать hardened.
4. На первом этапе допустим один agctl control-plane replica с persistent DB;
   для нескольких replicas нужны общая PostgreSQL и distributed runtime
   leases/locks. Текущие SQLite + in-memory active/TTL map не дают HA и не
   должны масштабироваться простым увеличением replicas.
5. R2 server dev-workspaces также требуют K8s адаптера: отдельные PVC для repo
   и DSH_HOME, Git bare storage/service и service-DNS gateway. Их текущие
   Docker `HostPort`/host bind mounts не переносятся напрямую.

KEDA — autoscaler workload replicas, не per-user sandbox/controller; одной
`Deployment replicas: 0` недостаточно, чтобы создавать адресный Pod/PVC на
каждого пользователя. K8s ChatProvider prototype уже делает базовый
userID→Pod/Service/PVC ensure; controller/CRD, распределённые leases,
reconnect/recovery и live-cluster проверки ещё не реализованы. Рассмотреть
собственный controller/CRD или [Kubernetes Agent Sandbox](https://github.com/kubernetes-sigs/agent-sandbox).
StatefulSet тоже возможен, но отдельный StatefulSet на каждого пользователя —
не выбранный пока вариант.

Проверенные официальные примитивы: [StatefulSet и PVC lifecycle](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/),
[сохранение volumes после удаления Pod/StatefulSet](https://kubernetes.io/docs/tasks/run-application/delete-stateful-set/),
[RuntimeClass](https://kubernetes.io/docs/concepts/containers/runtime-class/),
[KEDA scale to zero](https://keda.sh/docs/latest/concepts/).
Таблица ресурсов в `docs/capacity.md` измерена на Docker/M4; K8s scheduler,
PVC attach и gVisor/Kata overhead там **не учтены**.
