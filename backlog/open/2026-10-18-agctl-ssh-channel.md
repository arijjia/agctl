---
schema: 1.0.0
id: agctl-ssh-channel
title: "SSH-канал воркспейса: sshd, ключи, agctl connect + Mutagen sync (концепция §9.2)"
status: in-progress
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
