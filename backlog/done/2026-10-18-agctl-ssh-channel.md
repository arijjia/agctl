---
schema: 1.0.0
id: agctl-ssh-channel
title: "SSH-канал воркспейса: sshd, ключи, agctl connect + Mutagen sync (концепция §9.2)"
status: done
priority: P1
size: L
area: image
repo: arijjia/agctl
created: 2026-10-18
updated: 2026-10-18
tags: ["ssh", "night-shift"]
depends: []
epic: night-dsh-plugins
---
## Описание

Каркас SSH-канала по концепции `agctl-hq/docs/agctl-platform-concept-v2.md`
§5.2/§9.2: контейнерный dsh — удалённая среда, локальный IDE подключается
SSH. Публичный ключ задаётся при создании (`sshPubKey`) или через
`PATCH /api/workspaces/{id}/ssh`; private key остаётся у пользователя.
sshd включается только при непустом ключе; парольный вход и forwarding
выключены. Порт контейнера 2222 публикуется Docker только на 127.0.0.1
хоста (удалённый клиент требует отдельного туннеля/VPN); номер возвращается
как `sshPort`. CLI `agctl connect --host --port --identity` запускает ssh;
`agctl sync create` запускает Mutagen `two-way-safe` между локальной папкой и
`/workspace` по SSH (VCS/секретные файлы исключены по умолчанию).

## Критерии готовности

- Образ содержит openssh-server; контейнер без ключа не запускает sshd.
- Ключ валидируется, хранится как публичный текст; только владелец workspace
  может менять его; на работающем месте смена/удаление ключа пересоздаёт
  контейнер; API возвращает только enabled/port.
- SSH-порт привязан к loopback, root допускается только public-key auth,
  пароли и forwarding отключены.
- `scripts/e2e-ssh.sh`: create с ключом → `ssh ... printf SSH_OK` → удалить
  ключ/проверить отключение → ротация ключа/повторный SSH → `agctl connect
  --print`; полный e2e должен быть зелёным.
- `agctl sync create --print` собирает Mutagen two-way-safe endpoint
  `<user>@<host>:<port>:/workspace`; `agctl sync list/terminate` управляют
  сессией. Команда сообщает об отсутствующем Mutagen без паники/секретов.
- README объясняет установку Mutagen CLI, SSH-туннель для удалённого host-only
  порта и что `.env*`/`.credentials.*`/`.ssh`/`node_modules` исключены.
- `go vet ./... && go test ./...` зелёные; README описывает host-only
  границу доступа и туннельный сценарий.

## Журнал

- 2026-10-18: создано (`gitctl backlog new`).
- 2026-10-18: Старт реализации после закрытия cabinet-plugin-in-image и cabinet-panel-e2e.
- 2026-10-19: Реализация завершена: sshd key-only opt-in, API create/PATCH, migration 006, `agctl connect`, Mutagen `agctl sync create/list/pause/resume/flush/terminate`; loopback-only port 2222. Верификация: `go vet ./...`, `go test -race ./...`; `scripts/e2e-ssh.sh` PASS=7/7 (SSH auth, loopback, connect render, key disable/rotation), `scripts/e2e-cabinet.sh` PASS=6/6, `e2e-ssh` контейнеры/тестовые ключи очищаются; migration v5→v6 и owner-only/malformed-key tests зелёные. `agctl sync create --print` проверен на итоговом argv; Mutagen CLI в среде не установлен, поэтому реальную синхронизацию через Mutagen не прогонял (команда корректно сообщает об отсутствии бинаря).
- 2026-10-18: Верификация PASS: , ok  	github.com/arijjia/agctl/cmd/agctl	(cached)
ok  	github.com/arijjia/agctl/internal/assets	(cached)
ok  	github.com/arijjia/agctl/internal/auth	(cached)
?   	github.com/arijjia/agctl/internal/backlog	[no test files]
ok  	github.com/arijjia/agctl/internal/provision	(cached)
ok  	github.com/arijjia/agctl/internal/runtime/dockerrt	(cached)
ok  	github.com/arijjia/agctl/internal/secrets	(cached)
ok  	github.com/arijjia/agctl/internal/server	11.656s
ok  	github.com/arijjia/agctl/internal/store	(cached)
?   	github.com/arijjia/agctl/web	[no test files];   ok: workspace provisioning accepted with sshPubKey
  ok: workspace running; sshPort=57147
  ok: Docker SSH bind loopback-only (127.0.0.1:57147)
  ok: public-key SSH command executed in container
  ok: agctl connect renders ssh command
  ok: SSH key removal recreated container and disabled port
  ok: key rotation recreated container and authenticates new key
== e2e-ssh result: PASS=7 FAIL=0 (home=/tmp/agctl-ssh-e2e-20261019-005052) == PASS=7/7 (Docker bind 127.0.0.1, public-key shell, connect render, remove key disables, rotate key reconnects); == e2e-cabinet 20261019-005100 (img=agctl-dsh:0.2.0-night1) ==
  ok: agctl serve: регистрация+сессия
  ok: воркспейс создан и running (w-1d62837dd0ca running)
  ok: без ключа SSH отключён по умолчанию
  ok: без ключа контейнерный порт 2222 не опубликован
  ok: шлюз /w/<id>: boot graph содержит @arijjia/agctl-cabinet
  ok: night-profile: панель в boot graph прямого dsh web (:3556)
== итог: PASS=6 FAIL=0 == PASS=6/6 (панель, default SSH-off, no 2222 publish); migration v5→v6 test; owner-only/malformed-key tests.  проверен: двухсторонний Mutagen endpoint , two-way-safe, secret/VCS ignores; Mutagen CLI не установлен в этой среде, поэтому реальный continuous sync не запускал; missing CLI guard проверен.
- 2026-10-18: done — Optional key-only SSH в контейнере, loopback port, API create/PATCH, CLI connect + Mutagen sync bootstrap, e2e SSH rotation и security tests; реальный Mutagen CLI требует отдельной установки пользователем.
- 2026-10-18: CORRECTION: prior journal line was shell-expanded because command names were wrapped in backticks. Verified facts: go vet ./... and go test -race ./... PASS; scripts/e2e-ssh.sh PASS=7/7 (loopback bind, public-key SSH, key removal, key rotation); scripts/e2e-cabinet.sh PASS=6/6; migration v5-to-v6, owner-only, malformed-key tests PASS. Mutagen CLI is not installed here: agctl sync create --print output verified the root@127.0.0.1:port:/workspace endpoint, two-way-safe mode, and secret/VCS ignores; the missing-binary guard is clear. Actual live Mutagen file synchronization was NOT run.
