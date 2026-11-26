package main

// usage.go — справка agctl (бинарь разработчика). Содержимое меняется
// синхронно с командами в main() и completion.go.

import "github.com/arijjia/agctl/internal/cli"

// help — команды agctl: локальный режим и служебные.
var help = cli.Usage{
	Tagline: "платформа агентских рабочих мест на своей машине",
	Sections: []cli.UsageSection{
		{
			Title: "Локальный режим",
			Rows: []cli.UsageRow{
				{Name: "dev init", Desc: []string{"настроить: профиль, образ, папка проектов, доступы Jira/GitLab/LLM"}},
				{Name: "dev up [DIR]", Desc: []string{
					"поднять платформу и рабочее место (DIR → /workspace), открыть браузер",
					"dev down · status · open · logs · secrets · bridge · update · rollback · doctor",
				}},
				{Name: "dev paths", Desc: []string{"где что лежит: корень, бинари, профиль, дом и его файлы, порты, контейнеры, маунты, секреты"}},
				{Name: "dev migrate", Desc: []string{"перенести прежние дома ~/.agctl-dev[-<id>] в <корень>/homes/<id> (план; --apply — выполнить)"}},
			},
		},
		{
			Title: "Служебные",
			Rows: []cli.UsageRow{
				{Name: "backlog", Desc: []string{"карточки задач: new|list|top|check|start|done|cancel|note|migrate"}},
				{Name: "completion fish", Desc: []string{"скрипт автодополнения для fish"}},
				{Name: "version", Desc: []string{"версия и сборка"}},
				{Name: "help", Desc: []string{"эта справка"}},
			},
		},
	},
	Env: []cli.UsageRow{
		{Name: "AGCTL_ROOT", Desc: []string{"корень локального режима: bin/, profiles/<id>/, homes/<id>/ (по умолчанию ~/.agctl)"}},
		{Name: "AGCTL_HOME_ID", Desc: []string{"id дома (то же, что agctl dev --home <id>); без него — единственный дом или agctl"}},
		{Name: "AGCTL_DEV_HOME", Desc: []string{"абсолютный путь дома вне корня — для тестов и e2e"}},
	},
	Footer: "Сервер организации — отдельный бинарь agctl-server.",
}
