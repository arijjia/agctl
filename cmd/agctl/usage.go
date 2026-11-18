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
		{Name: "AGCTL_DEV_HOME", Desc: []string{"дом локального режима (по умолчанию ~/.agctl-dev)"}},
	},
	Footer: "Сервер организации — отдельный бинарь agctl-server.",
}
