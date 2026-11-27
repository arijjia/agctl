package main

// completion.go — `agctl-server completion fish`: скрипт автодополнения для fish.
// Скрипт статический (команды/флаги стабильны), генерируется из того же
// знания, что и справка: менять их надо синхронно.
//
//	agctl-server completion fish > ~/.config/fish/completions/agctl-server.fish

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

// fishCompletionScript — автодополнение fish для agctl-server.
const fishCompletionScript = `# agctl-server — автодополнение fish. Сгенерировано 'agctl-server completion fish'.
# Установка: agctl-server completion fish > ~/.config/fish/completions/agctl-server.fish
# Обновить после смены команд/флагов agctl-server — перегенерировать той же командой.

# Контекст второго уровня: подкоманда sync ещё не введена.
function __agctl_server_sync_needs_sub
    __fish_seen_subcommand_from sync; and not __fish_seen_subcommand_from create list pause resume flush terminate
end


# Позиционные слова командной строки (без флагов) после "agctl-server".
# __agctl_server_at a b  — ровно путь «a b» (пора предлагать следующую подкоманду);
# __agctl_server_in a b  — путь начинается с «a b» (флаги этой подкоманды).
function __agctl_server_words
    set -l t (commandline -opc)
    set -e t[1]
    for w in $t
        string match -q -- '-*' $w; or echo $w
    end
end
function __agctl_server_at
    set -l w (__agctl_server_words)
    test (count $w) -eq (count $argv); or return 1
    for i in (seq (count $argv))
        test "$w[$i]" = "$argv[$i]"; or return 1
    end
end
function __agctl_server_in
    set -l w (__agctl_server_words)
    test (count $w) -ge (count $argv); or return 1
    for i in (seq (count $argv))
        test "$w[$i]" = "$argv[$i]"; or return 1
    end
end

# ── подкоманды верхнего уровня ───────────────────────────────────────────────
complete -c agctl-server -n '__fish_use_subcommand' -x -a serve -d '⚠️ поднять UI+API+provision платформы'
complete -c agctl-server -n '__fish_use_subcommand' -x -a migrate -d '✏️ накатить миграции SQLite вручную'
complete -c agctl-server -n '__fish_use_subcommand' -x -a seed-demo-users -d '✏️ локальные тестовые роли user/developer/admin'
complete -c agctl-server -n '__fish_use_subcommand' -x -a connect -d '⚠️ SSH в персональный контейнер (роль developer)'
complete -c agctl-server -n '__fish_use_subcommand' -x -a sync -d 'Mutagen-синхронизация папки с /workspace'
complete -c agctl-server -n '__fish_use_subcommand' -x -a config -d '🔍 эффективный config.yaml'
complete -c agctl-server -n '__fish_use_subcommand' -x -a version -d '🔍 версия и сборка'
complete -c agctl-server -n '__fish_use_subcommand' -x -a completion -d '🔍 скрипт автодополнения (fish)'
complete -c agctl-server -n '__fish_use_subcommand' -x -a help -d '🔍 справка'

# ── serve ────────────────────────────────────────────────────────────────────
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l home -x -a '(__fish_complete_directories)' -d 'каталог данных платформы'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l addr -x -d 'адрес UI/API (host:port)'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l image -x -d 'тег серверного образа agctl-dsh-server'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l bin-dir -x -a '(__fish_complete_directories)' -d 'каталог linux-сборок для образа'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l profile -x -d 'профиль платформы: agctl или id профиля организации'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l profile-dir -x -a '(__fish_complete_directories)' -d 'каталог профиля с диска'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l llm-base-url -x -d 'базовый URL OpenAI-совместимого провайдера'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l llm-model -x -d 'модель чата по умолчанию'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l llm-models -x -d 'список моделей через запятую'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l llm-api-key -x -d 'ключ провайдера чата (лучше через env)'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l chat-ttl-min -x -d 'минуты простоя до сна контейнера'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l mem-gb -x -d 'лимит памяти контейнера, ГБ'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l cpus -x -d 'лимит CPU контейнера'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l cors-origin -x -d 'разрешённые CORS origins через запятую'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l dev-images -x -d 'образы лаунчера developer (через запятую)'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l dev-env -x -d 'env рабочего места developer'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l public-url -x -d 'внешний URL платформы'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l register-per-min -x -d 'лимит регистраций в минуту'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l secure-cookies -d 'Secure cookie (за доверенным TLS-терминатором)'
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l tier0 -d 'общий чат Tier 0 в корне платформы'

complete -c agctl-server -n '__fish_seen_subcommand_from migrate seed-demo-users' -l home -x -a '(__fish_complete_directories)' -d 'каталог данных платформы'

# ── connect ──────────────────────────────────────────────────────────────────
complete -c agctl-server -n '__fish_seen_subcommand_from connect' -l port -x -d 'SSH-порт контейнера (из кабинета/API)'
complete -c agctl-server -n '__fish_seen_subcommand_from connect' -l host -x -d 'адрес хоста agctl'
complete -c agctl-server -n '__fish_seen_subcommand_from connect' -l user -x -d 'SSH-пользователь внутри контейнера'
complete -c agctl-server -n '__fish_seen_subcommand_from connect' -l identity -x -r -d 'путь к приватному ключу'
complete -c agctl-server -n '__fish_seen_subcommand_from connect' -l print -d 'только напечатать ssh-команду'

# ── sync: подкоманды и флаги ─────────────────────────────────────────────────
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a create -d '✏️ создать Mutagen-сессию'
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a list -d '🔍 список сессий'
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a pause -d '⚠️ пауза сессии'
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a resume -d '⚠️ возобновить сессию'
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a flush -d '✏️ сброс буферов на диск'
complete -c agctl-server -n '__agctl_server_sync_needs_sub' -x -a terminate -d '✏️ удалить сессию'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l name -x -d 'уникальное имя Mutagen-сессии'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l local -x -a '(__fish_complete_directories)' -d 'локальная директория проекта'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l host -x -d 'SSH-хост'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l port -x -d 'sshPort из API/кабинета'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l user -x -d 'SSH-пользователь контейнера'
complete -c agctl-server -n '__fish_seen_subcommand_from sync; and __fish_seen_subcommand_from create' -l print -d 'только вывести mutagen-команду'

# ── completion ───────────────────────────────────────────────────────────────
complete -c agctl-server -n '__agctl_server_at completion' -x -a fish -d '🔍 скрипт для fish'

# ── serve: бэкенды рантаймов ──────────────────────────────────────────────────
complete -c agctl-server -n '__fish_seen_subcommand_from serve' -l runtime -x -a 'docker k8s ax' -d 'бэкенд персональных рантаймов'
`
