# agctl v2 — архитектура, запуск и дорожная карта

> Актуально на 2026-10-19. Стратегическая концепция и теория сред — в штабе:
> `agctl-hq/docs/agctl-platform-concept-v2.md` и
> `agctl-hq/docs/agctl-sandbox-and-runtime-theory.md`. Предыдущий план v1 —
> в истории git (файл `docs/agctl-architecture-roadmap.md` до коммита слияния
> ночи 2026-10-18/19).

## 1. Что такое agctl v2

**Платформа «ИИ для всей организации» — один Go-бинарь.** Два уровня в одном
продукте:

- **Tier 0 — чат для всех.** Открыл адрес платформы → dsh web с агентом и
  инструментами, ноль настроек. Дефолтный опыт — обычный чат: вопросы без
  инструментов не требуют среды; при первом инструменте среда создаётся лениво.
- **Tier 2 — инженерное место.** Кнопка «создать рабочее место» → персональный
  git-репозиторий (bare + клон) и Docker-контейнер с DeepSeek Harness
  (`dsh web`): файловая система, терминал, git, скиллы, MCP, SSH-канал.
  Агент коммитит работу в git — репа становится версионированной памятью.

Между ними — Tier 1 (кабинет с личными файлами и коннекторами) по концепции
v2.1; сегодня Tier 1 реализован как «кабинет-плагин + личное облако файлов»
(см. §3).

## 2. Компоненты (какие файлы за что отвечают)

```
cmd/agctl/            serve, migrate, backlog, workspace, connect, sync, version
internal/server/      HTTP: REST API, шлюз /w/{id}/, CORS, кабинет-логика
internal/provision/   git (bare+клон), комплекты (kits), SeedDshHome
internal/runtime/     RuntimeIface → dockerrt (Docker Engine API); k3s — следующий
internal/store/       SQLite (modernc, без cgo) + миграции 001..006
internal/assets/      go:embed: шаблоны комплектов, dsh-skel, контекст образа
internal/auth|secrets argon2id-сессии; AES-256-GCM хранилище токенов
web/                  React SPA кабинета (go:embed в бинарь)
dsh-plugins/cabinet/  клиент-плагин dsh web «Кабинет» — сеется в каждый воркспейс
scripts/              e2e-cabinet.sh, e2e-ssh.sh, capacity-bench*.sh, night-profile.sh
```

## 3. Кабинет-плагин (панель в dsh web)

`dsh-plugins/cabinet` — клиент-плагин DSH: регистрирует панель «Кабинет» в
`sidebar.panellist` и страницу в keyed slot `main` (паттерн
`dsh-client-ui-plugin-manager`). Устанавливается **в скелет dsh-home**
(`internal/assets/dsh-skel/...`), поэтому каждый воркспейс получает панель
«из коробки»; анти-дрейф тест `internal/assets/assets_test.go` сверяет копию
с исходником байт-в-байт.

Панель: вход через `agctl` (CORS или same-origin за шлюзом `/w/{id}/`),
список/создание/старт/стоп мест, опциональный SSH-ключ, вывод sshPort.
Ключевые фиксы волны 2: `/api/me` возвращает `user`-объект; форма создания
требует Base URL/модель и передаёт `sshPubKey`.

## 4. Сети и доступ

| Канал | Как работает | Ограничение |
|---|---|---|
| Кабинет-админка | `agctl serve` → SPA на `--addr` (по умолчанию `127.0.0.1:8929`) | TLS терминирует ingress, `--secure-cookies` |
| dsh web воркспейса | шлюз `/w/{id}/`: сессия+владелец на каждый запрос, reverse-proxy WS/SSE, cookie-mint boot-токена | детали `docs/secure-gateway.md` |
| Кабинет-плагин → agctl | same-origin (за шлюзом) или явный URL + `serve --cors-origin` (allow-list, по умолчанию выкл) | wildcard с credentials запрещён |
| SSH-канал (opt-in) | `sshd` в контейнере только при ключе; порт 2222 публикуется **только на 127.0.0.1** | удалённый клиент — через туннель/VPN |
| Mutagen sync | `agctl sync create` → two-way-safe local↔/workspace по SSH | VCS/секреты исключены |

## 5. SSH-канал и sync

- Ключ: при создании (`sshPubKey`) или `PATCH /api/workspaces/{id}/ssh`
  (валидация через `x/crypto/ssh`, owner-only, guard на provisioning).
- Хранение: колонки `ssh_pub_key`, `ssh_port` (миграция 006, есть upgrade-тест
  v5→v6). На работающем месте смена ключа пересоздаёт контейнер.
- sshd: root + `prohibit-password`, `AllowTcpForwarding no`, `X11Forwarding no`;
  без ключа sshd не запускается и порт 2222 не публикуется вовсе.
- CLI: `agctl connect --host 127.0.0.1 --port <sshPort> --identity KEY`
  (или `--print`), `agctl sync create/list/…` (Mutagen; при отсутствии —
  понятная ошибка 127). Для удалённой машины нужен туннель: SSH-порт намеренно
  не открыт в LAN/Internet.

## 6. Тестирование

| Уровень | Команда | Что покрывает |
|---|---|---|
| Юнит+интеграция | `go test ./...` (`-race` для server/store/runtime) | API, шлюз, CORS, миграции, provision, мок-рантайм |
| Анти-дрейф | `go test ./internal/assets` | скелет-копия плагина == исходник |
| Docker e2e | `IMG=… scripts/e2e-cabinet.sh` | образ → workspace → панель через шлюз; SSH off by default |
| Docker e2e SSH | `IMG=… scripts/e2e-ssh.sh` | ключ → sshd → реальный ssh-shell → удаление/ротация; опц. фаза real-Mutagen |
| Браузерный | `tests/e2e-ui` (Playwright) | панель в сайдбаре, CORS-логин, создание места через UI |
| Ёмкость | `scripts/capacity-bench*.sh` | плотность воркспейсов; цена `dsh headless` на задачу |

Замеры 2026-10-18/19 (ночь, M4/24GB, стаб LLM): воркспейс cold ~1 с,
idle ~125 МБ, `dsh headless` на задачу ~0,5 с/~200 МБ, par4 ~0,7 с/800 МБ.

## 7. Дорожная карта

Готово (v2, ночь 2026-10-18/19 + ранее): контроль-плейн/шлюз, kits, e2e-smoke,
SSH-канал, кабинет-плагин из коробки, CORS, Mutagen-bootstrap, ёмкостные
замеры, Playwright-пакет.

Дальше (приоритет владельца):

1. CI: гонять `e2e-cabinet.sh`/`e2e-ssh.sh` и Playwright в Actions.
2. Tier 1: личные файлы + personal RAG в кабинете.
3. MCP-шлюз к корпоративным системам (LMS, Jira, GitLab) с capability-грантами.
4. k3s-рантайм поверх существующего `RuntimeIface`; suspend/resume (Substrate)
   — кандидат на плотность.
5. Эфемерные серверные среды для Tier 0/1 (`dsh headless` на задачу).
6. Удаление/переименование мест и управление провайдерами из панели кабинета.

## 8. Безопасность — краткий статус

Изоляция контейнером; секреты AES-256-GCM; argon2id; шлюз с проверкой на
каждый запрос; SSH root — только ключи, forwarding запрещён; egress по
умолчанию не ограничен (кандидат — NetworkPolicy в k3s). Подробности шлюза —
`docs/secure-gateway.md`; стратегия — концепция v2 §6 (в штабе).
