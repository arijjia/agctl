# Кастомизация agctl под свою организацию

agctl — универсальная платформа: ядро не содержит брендинга и дефолтов
конкретной организации (это проверяет guard-тест
`TestCoreIsOrganizationFree` в `internal/profile/guard_test.go`). Всё
кастомное живёт в **профиле** — каталоге данных, который подключается без
пересборки бинаря.

## 1. Состав профиля

```
my-org/
  profile.yaml      # тексты, дефолты, сценарии, образы
  assets/logo.png   # логотип (опционально)
  catalog/<id>/     # пакеты возможностей воркспейса (опционально)
    manifest.json   #   {"id","title","description"}
    files/**        #   файлы 1:1 в воркспейс (скиллы — в .dsh/skills/<name>/)
  image/Dockerfile  # надстройка образа рабочего места (опционально)
```

Всё содержимое организации живёт только в её профиле: брендинг, модели,
готовые сценарии, пакеты каталога и пакеты образа. Ядро своих сценариев и
пакетов не несёт: профиль без `workspace.scenarios` и `catalog/` показывает
пустые списки, и чужие сценарии в него не попадают.

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
llm:                          # провайдеры чата (все поля опциональны)
  provider_name: my-org-llm   # алиас основного провайдера в выпадашке dsh (пусто = agctl)
  default_base_url: https://llm.my-org.example/v1
  default_model: my-fast
  default_models: my-fast,my-deep
  extra_providers:            # второй и следующие провайдеры в том же списке
    - name: corp-litellm
      base_url: https://litellm.my-org.example/v1
      model: corp-fast
      models: corp-fast,corp-deep
      api_key_env: AGCTL_LLM_CORP_API_KEY   # имя env, значение — только в .env хоста
help:
  cabinet_title: Справка — Моя организация
map:
  llm_label: LLM корп.        # подпись LLM на физической карте (/map)
examples:
  jira_url: https://jira.my-org.example
workspace:                    # содержимое рабочих мест организации
  scenarios:                  # кнопки «готовый сценарий» в диалоге создания воркспейса
    - id: my-scenario
      title: Мой сценарий
      description: клонировать репо-донор со скиллами и тулзами
      clone_url: https://github.com/my-org/my-scenario
images:
  dockerfile: image/Dockerfile   # надстройка FROM ${BASE} над agctl-dsh (опционально)
  dev:                           # каталог образов лаунчера developer'а
    - agctl-dsh-my-org:0.2.2
    - agctl-dsh-nossh-my-org:0.2.2
```

**Надстройка образа.** Пакеты организации (python-библиотеки, CLI) ставятся
не в базовый agctl-dsh, а в `image/Dockerfile` профиля:

```dockerfile
ARG BASE=agctl-dsh:0.2.2
# Скачивание — в стадии, не зависящей от BASE: её слой кэшируется.
FROM python:3.11-slim-bookworm AS wheels
ARG PIP_INDEX_URL=https://pypi.org/simple
RUN pip download --no-cache-dir --index-url "$PIP_INDEX_URL" \
      --only-binary=:all: --dest /wheels python-pptx

FROM ${BASE}
COPY --from=wheels /wheels /tmp/wheels
RUN pip3 install --no-cache-dir --break-system-packages --no-index \
      --find-links /tmp/wheels python-pptx && rm -rf /tmp/wheels
```

Не ставь пакеты из сети прямо в `RUN` после `FROM ${BASE}`. Базовый
agctl-dsh пересобирается на каждом коммите agctl (в нём бинарь `agctl`), и
такой слой будет заново качать пакеты при каждом `make up`. При медленном
CDN PyPI это десятки минут. В стадии `wheels` пакеты скачиваются один раз
(Python 3.11, как в agctl-dsh), а установка поверх нового BASE идёт офлайн
за секунды. Зеркало (Nexus организации) задаётся через `PIP_INDEX_URL`.

С надстройкой рабочий образ профиля называется `agctl-dsh-<id>:<тег>`.
`agctl serve` сам соберёт его поверх базового, если образа нет.
`make img_build PROFILE=<id>` собирает оба варианта: `agctl-dsh-<id>` и
`agctl-dsh-nossh-<id>`. Явный `--image` перекрывает профиль. Для k8s и AX
образ профиля пушится в реестр (`make img_push PROFILE=<id>`).

## 2. Подключение

```bash
# вариант А: каталогом напрямую (проще всего)
agctl serve --profile-dir /path/to/my-org

# вариант Б: по имени — профиль ищется в ./profiles/<id> и $AGCTL_HOME/profiles/<id>
mkdir -p profiles && cp -R my-org profiles/my-org
agctl serve --profile my-org

# вариант В: отдельный git-репозиторий профиля (agctl-profile-<id>)
make profile-fetch PROFILE_REPO=<git-url-или-локальный-путь>   # кладёт в ./profiles/<id>
                   # (<id> — имя репо без префикса agctl-profile-; иначе PROFILE_NAME=<id>)
make up            # PROFILE выбирается автоматически, если в ./profiles ровно один профиль
make up PROFILE=rshb    # явно — обязательно, когда профилей несколько
make up PROFILE=agctl   # нейтральный запуск без организации

# Kubernetes: профиль — ConfigMap (make k8s_profile PROFILE=<id>), см. docs/kubernetes.md
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

Секреты (API-ключи провайдеров) в профиль **не входят** — только env
(`AGCTL_LLM_API_KEY` для основного, плюс переменные из
`llm.extra_providers[].api_key_env`) или зашифрованный SQLite провайдер.

## 4. Как это устроено внутри (для разработчика)

| Слой | Что происходит |
|---|---|
| `internal/profile` | схема + загрузка (embed/диск), валидация, предвычисленный `View` (base64-логотип, JSON) |
| `cmd/agctl serve` | разрешает профиль до регистрации флагов — LLM-дефолты флагов приходят из профиля (env перекрывает) |
| `internal/server` | `Server.brand` — `profile.View`; инъекции `window.__AGCTL_BRAND__` + `<title>` на wire (`chatshell.go`, `htmlscan.go`) |
| `dsh-plugins/*` | потребители `window.__AGCTL_BRAND__` с нейтральным фолбэком (прямой заход в dsh мимо шлюза) |
| `internal/provision/catalog.go` | каталог пакетов читается из `catalog/` профиля (`View.FS`); нет каталога — пустой список |
| `internal/runtime/dockerrt` | `EnsureImage` собирает надстройку профиля поверх базового образа (`BaseImage` + `Overlay`) |
| `internal/profile/guard_test.go` | падает, если литералы организации протекли в код ядра |

Данные профиля агctl-репо не коммитятся (`profiles/` в `.gitignore`) —
профиль организации живёт в своём репозитории (пример: `agctl-profile-utmn`)
и может быть приватным независимо от ядра.

## 5. Живой пример: профиль AI Workspaces (ТюмГУ)

Репозиторий `arijjia/agctl-profile-utmn` (приватный, GitHub) — рабочий
профиль пилота: брендинг ТюмГУ + модельный контур подписки Z.AI GLM
Coding Plan (`zai-coding-cn`: `glm-5.3`, `glm-5.3-flash` дефолт,
`glm-4.5-flash`, `glm-4.5v`) и корпоративный LiteLLM (`utmn-litellm`:
`qwen-3.8-deep`, `qwen-3.8-fast`) — оба в одном выпадающем списке dsh.
Ключи в репо не хранятся — они в `.env` хоста (`AGCTL_LLM_API_KEY`,
`AGCTL_LLM_CORP_API_KEY`) и при первом старте шифруются в SQLite.

Полный онбординг «клонировал профиль → вписал ключ → `make up` →
платформа как у владельца» — в README того репозитория. Кратко:

```bash
git clone https://github.com/arijjia/agctl-profile-utmn profiles/utmn   # из checkout agctl
cat > .env <<'EOF'
AGCTL_LLM_API_KEY=<ключ основного провайдера>
AGCTL_LLM_CORP_API_KEY=<ключ дополнительного провайдера, если задан>
EOF
make up    # PROFILE=utmn выбирается автоматически, профиль уже на месте
```

## 6. Второй живой профиль: AI Workspaces (РСХБ)

Репозиторий `arijjia/agctl-profile-rshb` (приватный) — профиль для
Россельхозбанка: то же имя платформы **AI Workspaces**, слоган «РСХБ на
правильном пути», логотип РСХБ, модельный контур Z.AI GLM Coding Plan;
корпоративный OpenAI-совместимый шлюз банка заготовлен в `extra_providers`
(закомментирован до появления эндпоинта). Подключение — как у `utmn`:

```bash
git clone https://github.com/arijjia/agctl-profile-rshb profiles/rshb
make up PROFILE=rshb      # или make k8s_profile PROFILE=rshb в кластере
```

Два профиля рядом (`profiles/utmn`, `profiles/rshb`) — обычная ситуация
для разработчика платформы: `make up` без `PROFILE=` тогда поднимает
нейтральный agctl и подсказывает выбрать профиль явно. Guard-тест ядра
знает маркеры обеих организаций (`ТюмГУ`, `РСХБ`, `Россельхозбанк`).
