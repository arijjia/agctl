package main

// completion.go — `agctl completion fish`: скрипт автодополнения для fish.
// Скрипт статический (команды/флагы стабильны), генерируется из того же
// знания, что и usage(): менять их надо синхронно.
//
//	agctl completion fish > ~/.config/fish/completions/agctl.fish

import (
	"fmt"
	"os"
)

// cmdCompletion — диспетчер: сейчас поддержан только fish.
func cmdCompletion(args []string) {
	shell := "fish"
	if len(args) > 0 && args[0] != "" {
		shell = args[0]
	}
	switch shell {
	case "fish":
		fmt.Print(fishCompletionScript)
	default:
		fmt.Fprintf(os.Stderr, "completion: оболочка %q не поддержана (доступен fish)\n", shell)
		os.Exit(2)
	}
}

// fishCompletionScript — автодополнение fish для agctl.
const fishCompletionScript = `# agctl — автодополнение fish. Сгенерировано 'agctl completion fish'.
# Установка: agctl completion fish > ~/.config/fish/completions/agctl.fish
# Обновить после смены команд/флагов agctl — перегенерировать той же командой.

# Контексты второго уровня: подкоманды backlog/sync ещё не введены.
function __agctl_backlog_needs_sub
    __fish_seen_subcommand_from backlog; and not __fish_seen_subcommand_from new init list top check start done cancel note migrate
end
function __agctl_sync_needs_sub
    __fish_seen_subcommand_from sync; and not __fish_seen_subcommand_from create list pause resume flush terminate
end

# Позиционные слова командной строки (без флагов) после "agctl".
# __agctl_at a b  — ровно путь «a b» (пора предлагать следующую подкоманду);
# __agctl_in a b  — путь начинается с «a b» (флаги этой подкоманды).
function __agctl_words
    set -l t (commandline -opc)
    set -e t[1]
    for w in $t
        string match -q -- '-*' $w; or echo $w
    end
end
function __agctl_at
    set -l w (__agctl_words)
    test (count $w) -eq (count $argv); or return 1
    for i in (seq (count $argv))
        test "$w[$i]" = "$argv[$i]"; or return 1
    end
end
function __agctl_in
    set -l w (__agctl_words)
    test (count $w) -ge (count $argv); or return 1
    for i in (seq (count $argv))
        test "$w[$i]" = "$argv[$i]"; or return 1
    end
end

# ── подкоманды верхнего уровня ───────────────────────────────────────────────
complete -c agctl -n '__fish_use_subcommand' -x -a serve -d 'поднять UI+API+provision платформы'
complete -c agctl -n '__fish_use_subcommand' -x -a migrate -d 'накатить миграции SQLite вручную'
complete -c agctl -n '__fish_use_subcommand' -x -a seed-demo-users -d 'локальные тестовые роли user/developer/admin'
complete -c agctl -n '__fish_use_subcommand' -x -a backlog -d 'ведение бэклога (файлы-карточки open/done)'
complete -c agctl -n '__fish_use_subcommand' -x -a connect -d 'SSH в персональный контейнер (роль developer)'
complete -c agctl -n '__fish_use_subcommand' -x -a local -d 'локальный backend-коннектор (experimental)'
complete -c agctl -n '__fish_use_subcommand' -x -a sync -d 'Mutagen-синхронизация папки с /workspace'
complete -c agctl -n '__fish_use_subcommand' -x -a dev -d 'локальный режим: платформа на своей машине'
complete -c agctl -n '__fish_use_subcommand' -x -a bridge -d 'внутри контейнера: мост к хосту'
complete -c agctl -n '__fish_use_subcommand' -x -a config -d 'эффективный config.yaml'
complete -c agctl -n '__fish_use_subcommand' -x -a init-chat-home -d 'K8s initContainer: DSH_HOME на томе'
complete -c agctl -n '__fish_use_subcommand' -x -a ax-runner -d 'PID 1 песочницы AX (в образе)'
complete -c agctl -n '__fish_use_subcommand' -x -a version -d 'версия и сборка'
complete -c agctl -n '__fish_use_subcommand' -x -a completion -d 'скрипт автодополнения (fish)'
complete -c agctl -n '__fish_use_subcommand' -x -a help -d 'справка'

# ── serve ────────────────────────────────────────────────────────────────────
complete -c agctl -n '__fish_seen_subcommand_from serve' -l home -x -a '(__fish_complete_directories)' -d 'каталог данных agctl'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l addr -x -d 'адрес UI/API (host:port)'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l image -x -d 'тег образа agctl-dsh'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l bin-dir -x -a '(__fish_complete_directories)' -d 'каталог linux-сборок для образа'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l profile -x -d 'профиль платформы (agctl, utmn, …)'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l profile-dir -x -a '(__fish_complete_directories)' -d 'каталог профиля с диска'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l llm-base-url -x -d 'базовый URL OpenAI-совместимого провайдера'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l llm-model -x -d 'модель чата по умолчанию'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l llm-models -x -d 'список моделей через запятую'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l llm-api-key -x -d 'ключ провайдера чата (лучше через env)'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l chat-ttl-min -x -d 'минуты простоя до сна контейнера'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l mem-gb -x -d 'лимит памяти контейнера, ГБ'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l cpus -x -d 'лимит CPU контейнера'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l cors-origin -x -d 'разрешённые CORS origins через запятую'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l dev-images -x -d 'образы лаунчера developer (через запятую)'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l dev-env -x -d 'env рабочего места developer'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l public-url -x -d 'внешний URL платформы'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l register-per-min -x -d 'лимит регистраций в минуту'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l secure-cookies -d 'Secure cookie (за доверенным TLS-терминатором)'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l tier0 -d 'общий чат Tier 0 в корне платформы'
complete -c agctl -n '__fish_seen_subcommand_from serve' -l experimental-local-bridge -d 'experimental local connector API'

# ── migrate / seed-demo-users / init-chat-home ───────────────────────────────
complete -c agctl -n '__fish_seen_subcommand_from migrate seed-demo-users' -l home -x -a '(__fish_complete_directories)' -d 'каталог данных agctl'
complete -c agctl -n '__fish_seen_subcommand_from init-chat-home' -l home -x -a '(__fish_complete_directories)' -d 'каталог DSH_HOME на PVC'
complete -c agctl -n '__fish_seen_subcommand_from init-chat-home' -l refresh-provider -d 'обновить настройки провайдера'

# ── backlog: подкоманды ──────────────────────────────────────────────────────
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a new -d 'новая карточка'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a init -d 'новая карточка'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a list -d 'список карточек'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a top -d 'верх приоритета'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a check -d 'валидация шапок'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a start -d 'взять в работу (in-progress)'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a done -d 'закрыть карточку'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a cancel -d 'отменить карточку'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a note -d 'заметка в журнал'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a migrate -d 'дописать схему в старые карточки'

# ── backlog: флаги ───────────────────────────────────────────────────────────
complete -c agctl -n '__fish_seen_subcommand_from backlog' -l root -x -a '(__fish_complete_directories)' -d 'корень бэклога (по умолчанию поиск от cwd)'
complete -c agctl -n '__fish_seen_subcommand_from backlog' -l repo -x -d 'имя репозитория для шапок карточек'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l title -x -d 'название задачи (обязательно)'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l priority -x -a 'P1 P2 P3' -d 'приоритет'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l size -x -a 'S M L' -d 'размер'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l area -x -d 'домен задачи'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l tags -x -d 'теги через запятую'
complete -c agctl -n '__fish_seen_subcommand_from new init' -l desc -x -d 'текст в «Описание»'
complete -c agctl -n '__fish_seen_subcommand_from list' -l all -d 'включая done/cancelled'

# ── connect ──────────────────────────────────────────────────────────────────
complete -c agctl -n '__fish_seen_subcommand_from connect' -l port -x -d 'SSH-порт контейнера (из кабинета/API)'
complete -c agctl -n '__fish_seen_subcommand_from connect' -l host -x -d 'адрес хоста agctl'
complete -c agctl -n '__fish_seen_subcommand_from connect' -l user -x -d 'SSH-пользователь внутри контейнера'
complete -c agctl -n '__fish_seen_subcommand_from connect' -l identity -x -r -d 'путь к приватному ключу'
complete -c agctl -n '__fish_seen_subcommand_from connect' -l print -d 'только напечатать ssh-команду'

# ── local connect (experimental) ─────────────────────────────────────────────
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l server -x -d 'адрес платформы agctl'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l grant -x -d 'одноразовый pairing grant'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l credential-file -x -r -d 'файл durable-credential (0600)'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l local -x -d 'loopback-адрес локального dsh web'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l launch-dsh -d 'запустить локальный dsh web'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l dsh-bin -x -d 'бинарь локального dsh'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l dsh-port -x -d 'порт локального dsh web'
complete -c agctl -n '__fish_seen_subcommand_from local; and __fish_seen_subcommand_from connect' -l dsh-token -x -d 'boot-токен уже запущенного dsh'

# ── sync: подкоманды и флаги ─────────────────────────────────────────────────
complete -c agctl -n '__agctl_sync_needs_sub' -x -a create -d 'создать Mutagen-сессию'
complete -c agctl -n '__agctl_sync_needs_sub' -x -a list -d 'список сессий'
complete -c agctl -n '__agctl_sync_needs_sub' -x -a pause -d 'пауза сессии'
complete -c agctl -n '__agctl_sync_needs_sub' -x -a resume -d 'возобновить сессию'
complete -c agctl -n '__agctl_sync_needs_sub' -x -a flush -d 'сброс буферов на диск'
complete -c agctl -n '__agctl_sync_needs_sub' -x -a terminate -d 'удалить сессию'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l name -x -d 'уникальное имя Mutagen-сессии'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l local -x -a '(__fish_complete_directories)' -d 'локальная директория проекта'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l host -x -d 'SSH-хост'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l port -x -d 'sshPort из API/кабинета'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l user -x -d 'SSH-пользователь контейнера'
complete -c agctl -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l print -d 'только вывести mutagen-команду'

# ── dev: локальный режим ─────────────────────────────────────────────────────
complete -c agctl -n '__agctl_at dev' -x -a init -d 'настроить: профиль, образ, папка, доступы'
complete -c agctl -n '__agctl_at dev' -x -a up -d 'поднять платформу и рабочее место [DIR]'
complete -c agctl -n '__agctl_at dev' -x -a down -d 'остановить (данные сохраняются)'
complete -c agctl -n '__agctl_at dev' -x -a status -d 'адрес, образ, папка, мост'
complete -c agctl -n '__agctl_at dev' -x -a open -d 'открыть браузер (новая ссылка входа)'
complete -c agctl -n '__agctl_at dev' -x -a logs -d 'лог фонового процесса'
complete -c agctl -n '__agctl_at dev' -x -a secrets -d 'доступы в хранилище ОС'
complete -c agctl -n '__agctl_at dev' -x -a bridge -d 'мост к хосту: подтверждения и журнал'
complete -c agctl -n '__agctl_at dev' -x -a update -d 'обновить образ (бэкап, проверка, откат)'
complete -c agctl -n '__agctl_at dev' -x -a rollback -d 'вернуть предыдущий образ'
complete -c agctl -n '__agctl_at dev' -x -a doctor -d 'самодиагностика'
complete -c agctl -n '__agctl_at dev' -x -a help -d 'справка локального режима'
complete -c agctl -n '__agctl_in dev init' -l profile -x -a 'agctl' -d 'профиль организации'
complete -c agctl -n '__agctl_in dev init' -l profile-dir -x -a '(__fish_complete_directories)' -d 'каталог профиля с диска'
complete -c agctl -n '__agctl_in dev init' -l image -x -d 'образ рабочего места (тег)'
complete -c agctl -n '__agctl_in dev init' -l flavor -x -a 'web 1c' -d 'вкус образа из профиля'
complete -c agctl -n '__agctl_in dev init' -l workdir -x -a '(__fish_complete_directories)' -d 'папка проектов (→ /workspace)'
complete -c agctl -n '__agctl_in dev init' -l addr -x -d 'адрес UI (127.0.0.1:PORT)'
complete -c agctl -n '__agctl_in dev init' -l yes -d 'не спрашивать (только флаги)'
complete -c agctl -n '__agctl_in dev init' -l no-pull -d 'не тянуть образ сейчас'
complete -c agctl -n '__agctl_in dev up' -x -a '(__fish_complete_directories)'
complete -c agctl -n '__agctl_in dev up' -l no-open -d 'не открывать браузер'
complete -c agctl -n '__agctl_in dev up' -l foreground -d 'в текущем терминале (Ctrl+C — стоп)'
complete -c agctl -n '__agctl_in dev logs' -s f -d 'следить за логом'
complete -c agctl -n '__agctl_at dev secrets' -x -a list -d 'что задано и где хранится'
complete -c agctl -n '__agctl_at dev secrets' -x -a set -d 'задать значение ИМЯ'
complete -c agctl -n '__agctl_at dev secrets' -x -a rm -d 'удалить значение ИМЯ'
complete -c agctl -n '__agctl_at dev secrets set; or __agctl_at dev secrets rm' -x -a 'AGCTL_LLM_API_KEY GITLAB_URL GITLAB_TOKEN JIRA_URL JIRA_USER JIRA_API_TOKEN'
complete -c agctl -n '__agctl_in dev secrets set' -l stdin -d 'значение из stdin'
complete -c agctl -n '__agctl_in dev secrets set' -l plain -d 'несекретное значение (в dev.yaml)'
complete -c agctl -n '__agctl_at dev bridge' -x -a list -d 'инструменты белого списка'
complete -c agctl -n '__agctl_at dev bridge' -x -a pending -d 'вызовы, ждущие подтверждения'
complete -c agctl -n '__agctl_at dev bridge' -x -a approve -d 'подтвердить вызов ID'
complete -c agctl -n '__agctl_at dev bridge' -x -a deny -d 'отклонить вызов ID'
complete -c agctl -n '__agctl_at dev bridge' -x -a log -d 'журнал вызовов'
complete -c agctl -n '__agctl_in dev update' -l image -x -d 'целевой образ (тег)'
complete -c agctl -n '__agctl_in dev rollback' -l restore-data -d 'вернуть и сессии dsh из бэкапа'

# ── bridge (внутри контейнера) ───────────────────────────────────────────────
complete -c agctl -n '__agctl_at bridge' -x -a list -d 'инструменты моста (JSON)'
complete -c agctl -n '__agctl_at bridge' -x -a call -d 'вызвать ИНСТРУМЕНТ k=v …'
complete -c agctl -n '__agctl_at bridge' -x -a mcp -d 'MCP stdio-сервер моста'

# ── completion ───────────────────────────────────────────────────────────────
complete -c agctl -n '__agctl_at completion' -x -a fish -d 'скрипт для fish'
`
