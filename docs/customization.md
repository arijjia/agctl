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
```

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
