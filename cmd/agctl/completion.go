package main

// completion.go — `agctl completion fish`: скрипт автодополнения для fish.
// Скрипт статический (команды/флаги стабильны), генерируется из того же
// знания, что и справка: менять их надо синхронно.
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
# Метки (cli-std): 🔍 читающая · ✏️ мутирующая · ⚠️ с побочным эффектом (демон,
# контейнер, браузер, сеть); у смешанных групп метки нет.

# Контекст второго уровня: подкоманда backlog ещё не введена.
function __agctl_backlog_needs_sub
    __fish_seen_subcommand_from backlog; and not __fish_seen_subcommand_from new init list top check start done cancel note migrate
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
complete -c agctl -n '__fish_use_subcommand' -x -a backlog -d 'ведение бэклога (файлы-карточки open/done)'
complete -c agctl -n '__fish_use_subcommand' -x -a dev -d 'локальный режим: платформа на своей машине'
complete -c agctl -n '__fish_use_subcommand' -x -a version -d '🔍 версия и сборка'
complete -c agctl -n '__fish_use_subcommand' -x -a completion -d '🔍 скрипт автодополнения (fish)'
complete -c agctl -n '__fish_use_subcommand' -x -a help -d '🔍 справка'

# ── backlog: подкоманды ──────────────────────────────────────────────────────
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a new -d '✏️ новая карточка'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a init -d '✏️ новая карточка'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a list -d '🔍 список карточек'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a top -d '🔍 верх приоритета'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a check -d '🔍 валидация шапок'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a start -d '✏️ взять в работу (in-progress)'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a done -d '✏️ закрыть карточку'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a cancel -d '✏️ отменить карточку'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a note -d '✏️ заметка в журнал'
complete -c agctl -n '__agctl_backlog_needs_sub' -x -a migrate -d '✏️ дописать схему в старые карточки'

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

# ── dev: локальный режим ─────────────────────────────────────────────────────
complete -c agctl -n '__agctl_at dev' -x -a init -d '✏️ настроить: профиль, образ, папка, доступы'
complete -c agctl -n '__agctl_at dev' -x -a up -d '⚠️ поднять платформу и рабочее место [DIR]'
complete -c agctl -n '__agctl_at dev' -x -a down -d '⚠️ остановить (данные сохраняются)'
complete -c agctl -n '__agctl_at dev' -x -a status -d '🔍 адрес, образ, папка, мост'
complete -c agctl -n '__agctl_at dev' -x -a open -d '⚠️ открыть браузер (новая ссылка входа)'
complete -c agctl -n '__agctl_at dev' -x -a logs -d '🔍 лог фонового процесса'
complete -c agctl -n '__agctl_at dev' -x -a secrets -d 'доступы в хранилище ОС'
complete -c agctl -n '__agctl_at dev' -x -a bridge -d 'мост к хосту: подтверждения и журнал'
complete -c agctl -n '__agctl_at dev' -x -a update -d '✏️ обновить образ (бэкап, проверка, откат)'
complete -c agctl -n '__agctl_at dev' -x -a rollback -d '✏️ вернуть предыдущий образ'
complete -c agctl -n '__agctl_at dev' -x -a doctor -d '⚠️ самодиагностика'
complete -c agctl -n '__agctl_at dev' -x -a paths -d '🔍 где что лежит: корень, профиль, дом, порты, контейнеры'
complete -c agctl -n '__agctl_at dev' -x -a migrate -d '✏️ перенести прежние дома ~/.agctl-dev в homes/'
complete -c agctl -n '__agctl_in dev' -l home -x -d 'id дома (каталог homes/<id>)'
complete -c agctl -n '__agctl_in dev paths' -l json -d 'машинный вывод'
complete -c agctl -n '__agctl_at dev' -x -a images -d '🔍 образы каталога: версия dsh, сборка guest, устаревшие'
complete -c agctl -n '__agctl_in dev images' -l json -d 'машинный вывод'
complete -c agctl -n '__agctl_at dev' -x -a llm-check -d '⚠️ модели: ответ и вызов инструментов'
complete -c agctl -n '__agctl_at dev' -x -a certs -d 'сертификаты организации для рабочих мест'
complete -c agctl -n '__agctl_at dev certs' -x -a list -d '🔍 что войдёт в набор сертификатов'
complete -c agctl -n '__agctl_at dev certs' -x -a host -d '✏️ доверять сертификатам этой машины (on|off|profile)'
complete -c agctl -n '__agctl_at dev certs' -x -a fetch -d '✏️ цепочка сервера в набор (АДРЕС)'
complete -c agctl -n '__agctl_at dev certs' -x -a add -d '✏️ свой сертификат PEM в набор'
complete -c agctl -n '__agctl_at dev certs' -x -a rm -d '✏️ убрать файл из набора'
complete -c agctl -n '__agctl_at dev certs' -x -a check -d '⚠️ пройдёт ли TLS до адреса'
complete -c agctl -n '__agctl_at dev certs host' -x -a 'on off profile'
complete -c agctl -n '__agctl_in dev certs fetch' -l yes -d 'не спрашивать подтверждение'
complete -c agctl -n '__agctl_in dev llm-check' -x -l model -d 'одна модель'
complete -c agctl -n '__agctl_in dev llm-check' -x -l provider -d 'один провайдер'
complete -c agctl -n '__agctl_in dev llm-check' -l insecure -d 'без проверки TLS (диагностика)'
complete -c agctl -n '__agctl_in dev llm-check' -x -l timeout -d 'предел на запрос'
complete -c agctl -n '__agctl_in dev migrate' -l apply -d 'выполнить перенос (без флага — план)'
complete -c agctl -n '__agctl_in dev migrate' -l from -x -a '(__fish_complete_directories)' -d 'где искать прежние дома'
complete -c agctl -n '__agctl_at dev' -x -a help -d '🔍 справка локального режима'
complete -c agctl -n '__agctl_in dev init' -l profile -x -a 'agctl' -d 'профиль организации'
complete -c agctl -n '__agctl_in dev init' -l profile-dir -x -a '(__fish_complete_directories)' -d 'явный каталог профиля вместо <корень>/profiles/<id>'
complete -c agctl -n '__agctl_in dev init' -l image -x -d 'образ рабочего места (тег)'
complete -c agctl -n '__agctl_in dev init' -l flavor -x -a 'web 1c' -d 'вкус образа из профиля'
complete -c agctl -n '__agctl_in dev init' -l workdir -x -a '(__fish_complete_directories)' -d 'папка проектов (→ /workspace)'
complete -c agctl -n '__agctl_in dev init' -l addr -x -d 'адрес UI (127.0.0.1:PORT)'
complete -c agctl -n '__agctl_in dev init' -l yes -d 'не спрашивать (только флаги)'
complete -c agctl -n '__agctl_in dev init' -l no-pull -d 'не тянуть образ сейчас (с --cli)'
complete -c agctl -n '__agctl_in dev init' -l cli -d 'образ и доступы спросить в терминале, а не в лаунчере'
complete -c agctl -n '__agctl_in dev up' -x -a '(__fish_complete_directories)'
complete -c agctl -n '__agctl_in dev up' -l no-open -d 'не открывать браузер'
complete -c agctl -n '__agctl_in dev up' -l foreground -d 'в текущем терминале (Ctrl+C — стоп)'
complete -c agctl -n '__agctl_in dev logs' -s f -d 'следить за логом'
complete -c agctl -n '__agctl_at dev secrets' -x -a list -d '🔍 что задано и где хранится'
complete -c agctl -n '__agctl_at dev secrets' -x -a set -d '✏️ задать значение ИМЯ'
complete -c agctl -n '__agctl_at dev secrets' -x -a rm -d '✏️ удалить значение ИМЯ'
complete -c agctl -n '__agctl_at dev secrets set; or __agctl_at dev secrets rm' -x -a 'AGCTL_LLM_API_KEY GITLAB_URL GITLAB_TOKEN JIRA_URL JIRA_USER JIRA_API_TOKEN'
complete -c agctl -n '__agctl_in dev secrets set' -l stdin -d 'значение из stdin'
complete -c agctl -n '__agctl_in dev secrets set' -l plain -d 'несекретное значение (в config.yaml)'
complete -c agctl -n '__agctl_at dev bridge' -x -a list -d '🔍 инструменты белого списка'
complete -c agctl -n '__agctl_at dev bridge' -x -a pending -d '🔍 вызовы, ждущие подтверждения'
complete -c agctl -n '__agctl_at dev bridge' -x -a approve -d '✏️ подтвердить вызов ID'
complete -c agctl -n '__agctl_at dev bridge' -x -a deny -d '✏️ отклонить вызов ID'
complete -c agctl -n '__agctl_at dev bridge' -x -a log -d '🔍 журнал вызовов'
complete -c agctl -n '__agctl_in dev update' -l image -x -d 'целевой образ (тег)'
complete -c agctl -n '__agctl_in dev rollback' -l restore-data -d 'вернуть и сессии dsh из бэкапа'

# ── completion ───────────────────────────────────────────────────────────────
complete -c agctl -n '__agctl_at completion' -x -a fish -d '🔍 скрипт для fish'
`
