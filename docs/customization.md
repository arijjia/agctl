# Кастомизация agctl под свою организацию

agctl — универсальная платформа: ядро не содержит брендинга и дефолтов
конкретной организации (это проверяет guard-тест
`TestCoreIsOrganizationFree` в `internal/profile/guard_test.go`). Всё
кастомное живёт в **профиле** — каталоге данных, который подключается без
пересборки бинаря.

## 1. Состав профиля

```
my-org/
  profile.yaml      # тексты и дефолты
  assets/logo.png   # логотип (опционально)
```

`profile.yaml` (все поля):

```yaml
schema: 1
id: my-org                    # идентификатор профиля
brand:
  name: Моя организация       # имя платформы в UI
  tagline: наш слоган         # тэглайн вместо «Into the Unknown»
  title: Моя организация      # <title> вкладки (по умолчанию = name)
  letter: М                   # буква-знак, если нет логотипа (по умолчанию первая буква)
  logo: assets/logo.png       # путь внутри профиля (опционально)
login:
  subtitle: вход в личное рабочее место   # подзаголовок страницы входа
llm:                          # дефолты провайдера чата (все поля опциональны)
  default_base_url: https://llm.my-org.example/v1
  default_model: my-fast
  default_models: my-fast,my-deep
help:
  cabinet_title: Справка — Моя организация
map:
  llm_label: LLM корп.        # подпись LLM на физической карте (/map)
examples:
  jira_url: https://jira.my-org.example
```

## 2. Подключение

```bash
# вариант А: каталогом напрямую (проще всего)
agctl serve --profile-dir /path/to/my-org

# вариант Б: по имени — профиль ищется в ./profiles/<id> и $AGCTL_HOME/profiles/<id>
mkdir -p profiles && cp -R my-org profiles/my-org
agctl serve --profile my-org

# вариант В: отдельный git-репозиторий профиля (как agctl-profile-utmn)
make profile-fetch PROFILE_REPO=<git-url-или-локальный-путь>   # кладёт в ./profiles/utmn
make up            # PROFILE выбирается автоматически: utmn, если профиль получен
make up PROFILE=agctl   # нейтральный запуск без организации
```

env-эквиваленты: `AGCTL_PROFILE`, `AGCTL_PROFILE_DIR`. Порядок разрешения:
`--profile-dir` → встроенные профили (только нейтральный `agctl`) →
`./profiles/<id>` → `$AGCTL_HOME/profiles/<id>`.

## 3. Что брендируется

Один канал: шлюз agctl инжектит `window.__AGCTL_BRAND__` (JSON профиля) в
`<head>` до первого кадра и до загрузки плагинов. Его читают:

- **чат-шелл и dev-шелл** (`/chat/`, `/w/{id}`) — заголовок вкладки,
  строка идентичности (логотип или буква-знак вместо знака dsh), тэглайн;
- **страница входа** (SPA) — `<h1>` и подзаголовок;
- **плагины** `chat-brand` (бренд сайдбара, русский composer hint —
  универсальная i18n ядра, не кастомизация) и `cabinet` (заголовок справки);
- **страница останова воркспейса** и подпись LLM на физической карте `/map`.

Секреты (API-ключи провайдера) в профиль **не входят** — только env
(`AGCTL_LLM_API_KEY`) или зашифрованный SQLite провайдер, как и раньше.

## 4. Как это устроено внутри (для разработчика)

| Слой | Что происходит |
|---|---|
| `internal/profile` | схема + загрузка (embed/диск), валидация, предвычисленный `View` (base64-логотип, JSON) |
| `cmd/agctl serve` | разрешает профиль до регистрации флагов — LLM-дефолты флагов приходят из профиля (env перекрывает) |
| `internal/server` | `Server.brand` — `profile.View`; инъекции `window.__AGCTL_BRAND__` + `<title>` на wire (`chatshell.go`, `htmlscan.go`) |
| `dsh-plugins/*` | потребители `window.__AGCTL_BRAND__` с нейтральным фолбэком (прямой заход в dsh мимо шлюза) |
| `internal/profile/guard_test.go` | падает, если литералы организации протекли в код ядра |

Данные профиля агctl-репо не коммитятся (`profiles/` в `.gitignore`) —
профиль организации живёт в своём репозитории (пример: `agctl-profile-utmn`)
и может быть приватным независимо от ядра.
