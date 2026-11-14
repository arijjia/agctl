# agctl в Kubernetes: бэкенды k8s и ax (Google AX + Agent Substrate)

`agctl serve` умеет три бэкенда персональных рантаймов (`--runtime`,
env `AGCTL_RUNTIME`, `serve.runtime` в config.yaml):

| Бэкенд | Что такое рантайм пользователя | Данные пользователя | Сон по TTL | Где |
|---|---|---|---|---|
| `docker` (по умолчанию) | контейнер agctl-dsh на хосте шлюза | bind-mount `$AGCTL_HOME/chats/u<ID>` | `docker stop` | рабочая станция, один сервер |
| `k8s` | под agctl-dsh в namespace установки (`internal/runtime/k8srt`) | RWX-том, общий со шлюзом (или PVC на пользователя) | под удаляется, том живёт | любой кластер |
| `ax` | AX Task = актор Agent Substrate в gVisor/microVM-песочнице (`internal/runtime/axrt`) | долговечный том актора `/workspace` + снимки Substrate | `SuspendTask` (снимок) | кластер с Substrate и AX |

Контракт для сервера один — `runtime.ChatProvider` (`EnsureChat` /
`StopChat` / `LogsChat` / `HasActiveSSH`): шлюз не знает, Docker под ним,
под или актор. Адаптеры k8s/ax реализуют ещё `healthReporter`
(reconcile unhealthy-рантаймов) и `containerLister` (админский список
`/api/admin/containers`), а legacy `RuntimeIface` для них — заглушка
(workspace-места v2 в кластере не поддерживаются, их API и так отвечает 410).

## 1. Образы

Для кластера нужны два образа в реестре, доступном узлам:

| Образ | Из чего | Роль |
|---|---|---|
| `agctl-serve` | `deploy/serve.Dockerfile` (distroless, uid 1000, UI вшит) | шлюз: auth, роли, прокси, Кабинет, SQLite |
| `agctl-dsh` (+ `agctl-dsh-nossh`) | `internal/assets/image` — тот же, что для Docker | рабочее место: dsh web :3080, sshd :2222, `agctl init-chat-home`, `agctl ax-runner` |

```bash
make img_build                                  # agctl-dsh / agctl-dsh-nossh
make img_serve_build K8S_ARCH=amd64             # agctl-serve
make img_push REGISTRY=registry.example.com/agctl   # печатает digest agctl-dsh — он нужен бэкенду ax
```

## 2. Бэкенд `k8s`

### Что создаёт адаптер на пользователя

- **Pod** `agctl-chat-<hash RuntimeID>`: init-контейнер `seed-dsh-home`
  (`agctl init-chat-home` — скелет DSH_HOME, провайдер LLM, плагины, пресет
  разрешений роли) и контейнер `dsh` (readiness TCP :3080, опционально
  liveness `agctl-dsh-net health`). Метки `agctl.io/version`, `agctl.io/user-id`,
  `agctl.io/runtime-id`, аннотация образа: под другой версии/образа
  заменяется при следующем старте (данные не трогаются), а стартовый
  `SweepVersionDrift` сносит поды прошлой сборки, как Docker-свип.
- **Service** `chat-<…>` ClusterIP :80 → :3080 — Endpoint для шлюза
  (`http://chat-….<ns>.svc.cluster.local`).
- **NetworkPolicy** `net-<…>`: ingress на :3080 только с подов шлюза
  (`--k8s-gateway-selector`, по умолчанию `app.kubernetes.io/name=agctl`);
  egress не ограничивается (URL LLM задаёт пользователь/профиль) — политика
  egress кластера остаётся за оператором.
- **Secret** `llm-<hash StorageID>` с ключами провайдеров (основной +
  `extra_providers`), в под попадают через `secretKeyRef`.
- **Данные**: либо подкаталог `<StorageID>/` общего RWX-тома
  (`--k8s-shared-claim`), либо PVC `data-<hash StorageID>` на пользователя.

`EnsureChat` ждёт `Ready` пода (`--k8s-ready-timeout`, 180 с), упавший под
удаляет с ошибкой «retry»; `StopChat` удаляет под, сервисы и политику,
никогда — том и Secret; `HealthStatus` → `unhealthy` для удалённых/упавших/
`CrashLoopBackOff`/`ImagePullBackOff` подов (шлюз пересоздаёт их лениво).

### Общий том (рекомендуется)

Проектные API шлюза (файлы проекта, autosave go-git, инструкции,
каталог пакетов) читают каталог пользователя с диска шлюза
(`$AGCTL_HOME/chats/u<ID>/work`). В кластере это работает только если шлюз и
поды dsh видят один том: RWX-класс (NFS, CephFS, EFS, Filestore, Longhorn
RWX) → PVC `agctl-data` (`deploy/k8s/base/storage.yaml`), шлюз монтирует его
как `/var/lib/agctl/chats`, поды — `subPath: u<ID>/work` и `u<ID>/dsh-home`.
uid 1000 у обеих сторон. Без RWX задайте `AGCTL_K8S_SHARED_CLAIM=""`: у
каждого пользователя будет свой RWO PVC, чат и dev-рабочее место работают,
проектные API шлюза — нет.

### SSH developer'а

`--k8s-ssh-host <адрес узлов или L4-балансировщика>` включает SSH-канал:
под с ключом developer'а получает порт 2222, NodePort-сервис `ssh-<…>`
(только sshd, web наружу не публикуется), NetworkPolicy открывает 2222.
Кабинет показывает `ssh root@<ssh-host> -p <nodePort>`. sshd требует root
(`ssh-keygen -A`, privsep) — такой под работает от root с `drop ALL` и
явным набором capabilities (CHOWN, DAC_OVERRIDE, FOWNER, SETGID, SETUID,
SYS_CHROOT, KILL, AUDIT_WRITE); namespace должен допускать baseline Pod
Security, рекомендуется `--k8s-runtime-class gvisor`. Рипер перед TTL
проверяет живой сеанс через `pods/exec` (`ps -eo pid,args`) — RBAC базы
даёт `create pods/exec`.

### Установка

```bash
# 1. образы (см. §1), профиль организации и ключи
make profile-fetch PROFILE_REPO=git@github.com:arijjia/agctl-profile-rshb.git
make k8s_profile PROFILE=rshb          # ConfigMap agctl-profile (+ assets с логотипом)
make k8s_secret                        # Secret agctl-llm из .env (AGCTL_LLM_API_KEY, …)

# 2. манифесты: правим образы/StorageClass/ssh-host в deploy/k8s/base, затем
make k8s_render                        # посмотреть
make k8s_apply                         # kubectl apply -k deploy/k8s/base
make k8s_status
```

Снаружи шлюз публикуется Ingress'ом с TLS; тогда в args Deployment добавьте
`--secure-cookies`. HA нет: один под `agctl serve` + SQLite на RWO-томе
(`strategy: Recreate`), HA — отдельная волна (`docs/architecture-roadmap.md`).

Все флаги бэкенда: `--k8s-namespace` (пусто = namespace шлюза),
`--k8s-kubeconfig` (пусто = in-cluster), `--k8s-image`, `--k8s-pull-policy`,
`--k8s-storage-class`, `--k8s-storage-size`, `--k8s-runtime-class`,
`--k8s-shared-claim`, `--k8s-ssh-host`, `--k8s-ready-timeout`,
`--k8s-gateway-selector`, `--k8s-liveness`; лимиты пода — `--cpus`/`--mem-gb`
(те же, что у Docker). Env-эквиваленты `AGCTL_K8S_*`, config.yaml —
`serve.k8s.*`.

### Смоук в kind

`make k8s_e2e_kind` (`bin/e2e-k8s-kind.sh`): одноразовый kind-кластер, образы
через `kind load`, overlay `deploy/k8s/overlays/kind` (PVC на пользователя,
NodePort 30929), регистрация admin+user, `/chat/` от user → под dsh поднят и
отдаёт чат-шелл → `POST /api/me/runtime/stop` → под удалён, PVC цел. Нужны
docker, kind, kubectl. Если NodePort с хостом не доносится (Docker Desktop,
colima, nested docker) — скрипт сам падает на `kubectl port-forward`
(BASE_URL задаёт адрес явно).

## 3. Бэкенд `ax`: Google AX поверх Agent Substrate

### Маппинг понятий

| agctl | AX / Substrate |
|---|---|
| установка agctl | atespace (`--ax-atespace`, по умолчанию `agctl`) |
| пользователь (RuntimeID `chat-<home>-u<ID>`) | `Task` `agctl-chat-<home>-u<id>` = актор Substrate; образ `agctl-dsh@sha256:…`, команда `agctl-entrypoint`, env `AGCTL_LLM_*`, лимиты `--cpus`/`--mem-gb` |
| ленивый старт | `GetTask` → `CreateTask` (нет) → `ResumeTask` (не Running) → ожидание `Ready=True` (`--ax-ready-timeout`) |
| сон по TTL / стоп из лаунчера | `SuspendTask`: снимок актора (память/ФС/`/workspace`), воркер освобождается |
| Endpoint для шлюза | маршрутизатор Substrate `atenet-router` (`--ax-router-url`) + заголовок `ate-target-actor: <atespace>/<task>` на каждом запросе (`ChatHandle.Headers`) |
| boot-токен dsh (mint cookie) | `GET /agctl/boot-log` раннера через маршрутизатор, `Authorization: Bearer <токен раннера>` |
| health | `GetTask`: `Failed`/`Terminating`/нет задачи → unhealthy |
| админский список | `ListTasks` atespace |

Маршрутизатор умеет сам будить спящий актор по входящему запросу — поэтому
даже если шлюз перезапустился и забыл кэш, первый запрос пользователя дойдёт.

### Раннер в песочнице

AX всегда запускает контейнер командой `/usr/local/bin/ax-task-runner`
(PID 1), требует `/healthz` и `/readyz` на :80 и шлёт туда же трафик. В
образе agctl-dsh это обёртка над **`agctl ax-runner`** (`cmd/agctl/axrunner.go`):

1. читает `AX_TASK_YAML` (снисходительно), применяет `spec.env`;
2. готовит `DSH_HOME=/workspace/dsh-home` и `HOME=/workspace/work` на
   долговечном томе актора (скелет, провайдер, плагины, пресет роли — тот же
   набор, что в Docker и в init-контейнере k8s); провайдер — fetch-at-boot
   у шлюза (`GET /ax/provider-config/<task>` по `AGCTL_GATEWAY_URL` с
   Bearer `AGCTL_RUNNER_TOKEN`; недоступный шлюз в этом режиме — громкий
   fatal раннера), fallback — env задачи `AGCTL_LLM_*`
   (режим `AGCTL_AX_PROVIDER_AT_BOOT=0`);
3. запускает `spec.command` (по умолчанию `agctl-entrypoint` → dsh web на
   eth0:3080) в своей группе процессов, перезапускает при выходе с backoff,
   по SIGTERM — TERM группе, через 10 с KILL;
4. :80 — `/healthz` 200, `/readyz` 200 когда dsh слушает :3080 (иначе 503),
   `/agctl/boot-log?tail=N` — хвост вывода dsh по токену (`AGCTL_RUNNER_TOKEN`;
   без токена эндпоинт выключен), при `spec.debug` — guest-gRPC Substrate
   (h2c, см. ниже), всё остальное — реверс-прокси на dsh
   (WebSocket включительно); заголовки `Authorization`/`Ate-Target-Actor` до
   dsh не доходят.

SSH в режиме ax — guest-сервисы Substrate (п.5 карточки
k8s-substrate-ax-backend, опция A, opt-in + per-user). `ax ssh` апстрима —
это не sshd: раннер поднимает на том же :80 guest-gRPC Substrate
(`agent-substrate/env/guest`: process + filesystem, h2c рядом с
пробами/прокси — паттерн раннера апстрима AX), доступ к которому = произвольный
exec и файлы в песочнице. Включение установки — `--ax-debug-ssh` (env
`AGCTL_AX_DEBUG_SSH=1`, `serve.ax.debug_ssh`, по умолчанию выключено);
шлюз ставит `spec.debug` ТОЛЬКО задачам пользователей с SSH-ключом роли
developer/admin (`spec.SSHPubKey` ≠ "" — та же семантика, что «поднять sshd»
в docker/k8s; роли user guest-сервисов не получают никогда). Загрузка ключа
позже — дрейф specHash: пересоздание по `--ax-recreate-on-drift` или руками
(`ax delete task`). Раннер поднимает guest-сервисы (workspace = рабочий
каталог пользователя), оператор даёт доступ `ax ssh <task>` — с хоста
с сетевым доступом до воркеров или через `atenet-router` (443 mTLS / 80).
Кабинет по-прежнему показывает «спит» (`HasActiveSSH` = false: платформенного
ssh-канала нет) — терминал разработчика либо dsh web, либо `ax ssh` руками.

### Установка

1. **Substrate и AX** — по их README: `ate-setup deploy ate-system …`
   (Control API, atenet-router, SandboxConfig gVisor, snapshot storage),
   затем `make images && make deploy` в checkout AX (ax-server + PostgreSQL в
   `ax-system`). Версии: AX `9bf6aef` (2026-11-09, модуль
   `github.com/google/ax v0.3.2-0.20261109222931-9bf6aef197c2` — тот же, что
   в `go.mod` agctl) и Substrate из его `go.mod`.
2. **WorkerPool** для песочниц agctl — `deploy/ax/workerpool.yaml`
   (`ko apply` из checkout Substrate). Без пула первый resume падает с
   `ResourceExhausted`.
3. **Авторизация**: service account `ax-server` уже имеет права на atespace;
   SPIFFE-идентичность шлюза agctl (`spiffe://cluster.local/ns/agctl/sa/agctl`)
   добавьте в `--ingress-allowed-spiffe-ids` маршрутизатора, если он требует
   mTLS (тогда в overlay раскомментируйте PodCertificate/trust-bundle и
   `AGCTL_AX_ROUTER_URL=https://…:443`).
4. **Образ песочницы по digest**: `make img_push REGISTRY=…` печатает
   `…/agctl-dsh@sha256:…` → `AGCTL_AX_IMAGE` в `deploy/k8s/overlays/ax/patch-deployment.yaml`.
5. `make k8s_profile PROFILE=…`, `make k8s_secret`,
   `make k8s_apply K8S_OVERLAY=overlays/ax`.

### Ключи LLM: env задачи или egress-инъекция

По умолчанию (`--ax-inject-llm-key=1`) ключи провайдеров попадают в env
задачи — как в Docker/k8s, но env задачи AX хранится открыто и виден в
`ax get task`. Нулевое доверие Substrate предполагает иное:
`--ax-inject-llm-key=0` — ключей в песочнице нет, env задачи несёт
`placeholder`, dsh шлёт `Authorization: Bearer placeholder`, а egress-шлюз
Substrate (`atenet-egress`) подменяет заголовок реальным ключом из Secret
по `EgressPolicy` актора.

С `--ax-ate-api <addr>` (env `AGCTL_AX_ATE_API`, default
`serve.ax.ate_api`) agctl ведёт инъекцию сам: на каждом `EnsureChat`
синхронизирует Secret'ы `agctl-llm-*` с ключами провайдеров (in-cluster
service account шлюза, namespace пода) и создаёт/обновляет EgressPolicy
актора через Control API Substrate (`CreateActorEgressPolicy` /
`UpdateActorEgressPolicy` с uid/version-предусловиями; `--ax-ate-cluster`,
по умолчанию `cluster.local`, задаёт имя кластера в
`ate-secret://k8s.io/cluster/<cluster>/…`). Ротация ключа меняет Secret,
не трогая задачу и политику; смена провайдера обновляет правила политики.
Без `--ax-ate-api` политику создаёт оператор вручную
(`kubectl ate create egress-policy`, пример
`deploy/ax/egress-policy.example.yaml`); env задачи в этом режиме ключей
и placeholder'ов не содержит вовсе.

Требования автоматического режима: ate-api-server доступен шлюзу по gRPC
(внутри кластера; mTLS-обвязка — по роадмапу), развёрнут
credential-provider `kubernetes-secrets` (см. README Substrate) с правами
чтения Secret'ов namespace agctl. IP-адреса вместо DNS-имён в base URL
провайдера не поддерживаются паттернами правил — такие эндпоинты
остаются за ручной политикой оператора.

### Ограничения режима ax (v1)

- **Задача неизменяема.** Образ фиксируется при `CreateTask`; смена
  образа применится к новым пользователям, для старых шлюз пишет
  предупреждение. `--ax-recreate-on-drift` пересоздаёт задачу, но
  `/workspace` актора (проекты, история сессий) при этом теряется.
- **Конфиг провайдера — fetch-at-boot (по умолчанию).** Провайдер LLM не
  входит в env/spec задачи (`AGCTL_AX_PROVIDER_AT_BOOT=1`): env несёт
  только `AGCTL_GATEWAY_URL` (шлюз agctl, `--public-url`), а раннер при
  каждом boot забирает актуальный конфиг у шлюза —
  `GET /ax/provider-config/<task>` с Bearer-токеном раннера
  (`--ax-runner-token`; тот же, что у boot-log). Смена профиля/ключа
  применяется на следующем resume без пересоздания задачи — дрейфа specHash
  нет, `/workspace` сохраняется. Ключи в payload следуют режиму инъекции:
  `--ax-inject-llm-key=1` — реальный ключ, egress-автоматизация —
  `placeholder` (подменит atenet-egress), ручная политика — пусто (dsh
  спросит пользователя). Отключение (`AGCTL_AX_PROVIDER_AT_BOOT=0`)
  возвращает конфиг в env задачи (дрейф при смене провайдера).
- **Проектные API шлюза** (файлы, autosave, инструкции) данных актора не
  видят: пользователь работает с проектами внутри dsh web.
- **SSH** недоступен (см. раннер).
- **Данные = снимки Substrate.** Долговечность `/workspace` — это snapshot
  storage Substrate (`--snapshot-location`); резервное копирование — там.
- Live-проверка на кластере с Substrate/AX в этом репозитории не
  автоматизирована: адаптер покрыт тестами против in-memory ax-server
  (`internal/runtime/axrt/provider_test.go`), контракт раннера — тестами
  `cmd/agctl/axrunner_test.go`.

## 4. Профиль организации в кластере

Профиль монтируется каталогом `/etc/agctl/profile` (`AGCTL_PROFILE_DIR`):
ConfigMap `agctl-profile` (profile.yaml) и `agctl-profile-assets`
(`assets/logo.png`) — `make k8s_profile PROFILE=<id>` делает оба из
`profiles/<id>`. Для РСХБ: `make profile-fetch
PROFILE_REPO=git@github.com:arijjia/agctl-profile-rshb.git && make
k8s_profile PROFILE=rshb`.

## 5. Безопасность: что изменилось относительно Docker

| | docker | k8s | ax |
|---|---|---|---|
| изоляция рантайма | контейнер, cap drop | под non-root, seccomp RuntimeDefault, cap drop ALL (root + capabilities только с SSH), опц. gVisor RuntimeClass | gVisor/microVM-песочница Substrate, VM-grade |
| сеть к dsh | loopback хоста | NetworkPolicy: только поды шлюза | только через маршрутизатор; ingress-политика по SPIFFE |
| ключи LLM | env контейнера | k8s Secret → env пода | env задачи (видно в AX) **или** egress-инъекция (ключа в песочнице нет) |
| права шлюза | docker.sock (= root хоста) | Role на pods/services/pvc/secrets/netpol/exec в одном namespace | gRPC к ax-server, HTTP к маршрутизатору |
| boot-токен dsh | docker logs | pods/log | `/agctl/boot-log` раннера по токену установки (зашифрован в SQLite) |
