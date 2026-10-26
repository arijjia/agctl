# agctl v3 — архитектурный роадмап

> Срез: 2026-10-26, ветка `main`. Целевая модель владельца: один персональный
> рантайм на аккаунт, роли вместо двух режимов, проекты-папки, TTL 1 час.
> Фактическое устройство — [architecture.md](architecture.md); подробное
> обоснование — `agctl-hq/docs/v3/unified-runtime-model.md`; физика —
> `system/map/agctl-processes.html` (`make map`, `/map`). Живой список задач —
> `backlog/open|done` и эпик `v3-unified-runtime`; при расхождении правдивее
> карточки.

## 1. Реализовано: волны A–D

| Волна | Содержание | Статус |
|---|---|---|
| **A. Роль + единый runtime** | `role_requests` → admin approve → постоянная `users.role=developer`; SSH key-only в том же личном контейнере; TTL default 60 min; HEALTHCHECK :3099 + reconcile unhealthy | ✅ done |
| **B. Проекты + invisible Git** | `/api/projects`; реальный каталог + локальная `.git`; host plugin boot-регистрирует `/workspace/*` в DSH WorkspaceRegistry; native Workspaces browser + Add Workspace dialog (empty/GitHub); native Session cwd; go-git autosave без push | ✅ done |
| **C. Профиль/Кабинет v3.2** | Нижний user-chip popup (профиль/DSH Settings/help); role + requests + token projections/activity + SSH; никаких runtime controls и отдельного workspace списка | ✅ done |
| **D. Демонтаж seats** | UI создания мест/local-backend убран; создание/мутации legacy workspace и workspace-request → 410; CLI `agctl workspace create` отключена | ✅ done |

Пилот стартует с **чистого тома** — перенос старых `chats/` и `work/` не
выполняется. Для диагностических тестов/старых записей GET и `/w/{id}`
compat-proxy пока оставлены; это не пользовательский поток v3.

## 2. Следующий горизонт — надёжность и UX (P1/P2)

1. **Activity-aware TTL — active SSH sessions** (P1, `chat-activity-aware-ttl`):
   ✅ done. В последнюю минуту перед TTL Docker `ContainerTop` ищет sshd child;
   активная SSH/IDE-сессия продлевает lease, ошибка проверки fail-closed на тик.
   Detached-процесс, оставленный после закрытия SSH, не считается активным —
   отдельная возможная задача.
2. **Prewarm-пул** (P2, `chat-prewarm-pool`): подогрев к входу/началу занятий;
   сегодня контейнер создаётся лениво при `/chat/`, лимит тёплого пула,
   планировщик прогрева ещё не реализованы.
3. **Квоты тома и retention** (P2): ограничить диск/число проектов на
   пользователя, политика архивации/удаления и восстановления.
4. **Project templates** (P2): связать legacy `base`/`1c-vibe` kits с
   созданием v3-проектов; сегодня API создаёт каталог с README + Git, комплект
   ещё не применяется.
5. **Живой Playwright v3 smoke** (P2): роль→проект→сессия в project cwd→SSH;
   текущие проверки — Go tests, plugin mocks и одноразовая browser-проверка.
6. **Полное удаление legacy-кода** (P3, `legacy-workspace-code-cleanup`):
   выпилить dormant provision/seat tables и `/w` compat после срока пилота.

## 3. Безопасность (P1/P2)

- **Egress allowlist** (P2, `egress-allowlist`): сейчас egress Docker
  контейнеров открыт. Вводить opt-in правила только для LLM host/DNS и
  необходимых зависимостей; не менять глобальную Docker-сеть пользователя.
- **MFA/SSO** (P2/P3): сегодня argon2id + cookie; интеграция с корпоративным
  IdP — отдельная продуктовая волна.
- **Boot-token в Docker logs**: dsh сам печатает одноразовый token, agctl
  считывает его для mint. Docker daemon access равен админским правам; убрать
  секрет из upstream stdout без dsh-контракта/апстрим-изменения нельзя.
- **Control-plane HA** (P3): сейчас один `agctl serve` + SQLite; несколько
  replicas потребуют PostgreSQL, leases и согласованного runtime ownership.

## 4. Kubernetes и отложенные интеграции

- `internal/runtime/k8srt` — prototype одного Chat Pod/Service/PVC на user.
  Общий контракт `StorageID` сохраняет пользовательский том при смене Pod;
  `cmd serve` пока выбирает Docker, live cluster/readiness/NetworkPolicy
  не прошли production validation.
- K8s-путь v3: один Pod на пользователя (relay + dsh web + sshd для роли
  developer как sidecars в общей pod-сети) и один PVC; не Pod на проект.
- **Local backend** (DSH на ноутбуке) владелец решила осмыслить позже:
  серверный experimental code за выключенным флагом остаётся в репо, но
  UX/rollout не планировать без отдельного решения.

## 5. Принципы

1. Один контейнер на пользователя; role developer добавляет возможности,
   не множит runtime.
2. Персистентность в томе, контейнер — расходник с TTL 60 min и healthcheck.
3. Папка = реальный каталог проекта = DSH Workspace; новые сессии получают
   cwd проекта; существующие файлы между проектами автоматически не двигаются.
4. Git для user невидим, history локальная; remote/push появится только при
   отдельном решении.
5. dsh не форкаем; plugins/gateway/config — интеграционный слой.
6. Задачи — через backlog, код + тесты + статусы + документация в отдельных
   коммитах, без push без явной команды.

## 6. Работа по эпикам

Эпик `v3-unified-runtime` фиксирует решение и журнал волн. Следующая
реализация начинается с самой ранней открытой карточки с высоким приоритетом,
а статус в этом документе обновляется при закрытии волны.
