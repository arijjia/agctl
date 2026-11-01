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

# ── подкоманды верхнего уровня ───────────────────────────────────────────────
complete -c agctl -n '__fish_use_subcommand' -x -a serve -d 'поднять UI+API+provision платформы'
complete -c agctl -n '__fish_use_subcommand' -x -a migrate -d 'накатить миграции SQLite вручную'
complete -c agctl -n '__fish_use_subcommand' -x -a seed-demo-users -d 'локальные тестовые роли user/developer/admin'
complete -c agctl -n '__fish_use_subcommand' -x -a backlog -d 'ведение бэклога (файлы-карточки open/done)'
complete -c agctl -n '__fish_use_subcommand' -x -a connect -d 'SSH в персональный контейнер (роль developer)'
complete -c agctl -n '__fish_use_subcommand' -x -a local -d 'локальный backend-коннектор (experimental)'
complete -c agctl -n '__fish_use_subcommand' -x -a sync -d 'Mutagen-синхронизация папки с /workspace'
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
`
