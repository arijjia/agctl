package main

// usage.go — справка agctl-server (сервер организации). Содержимое меняется
// синхронно с командами в main() и completion.go.

import "github.com/arijjia/agctl/internal/cli"

// help — команды agctl-server: управление платформой, работа developer'а,
// служебные.
var help = cli.Usage{
	Tagline: "сервер платформы агентских рабочих мест",
	Sections: []cli.UsageSection{
		{
			Title: "Управление платформой",
			Rows: []cli.UsageRow{
				{Name: "serve", Desc: []string{
					"⚠️ поднять платформу: UI + API + личные runtime пользователей",
					"--home DIR · --addr H:P · --image T · --bin-dir D · --profile NAME · --llm-*",
					"--runtime docker|k8s|ax · --k8s-* · --ax-* (agctl-hq/docs/kubernetes.md)",
				}},
				{Name: "migrate", Desc: []string{"✏️ накатить миграции SQLite вручную"}},
				{Name: "seed-demo-users", Desc: []string{"✏️ локальные тестовые роли user/developer/admin (после первого admin)"}},
				{Name: "config", Desc: []string{"🔍 показать эффективный ~/.config/agctl/config.yaml"}},
			},
		},
		{
			Title: "Работа developer'а",
			Rows: []cli.UsageRow{
				{Name: "connect", Desc: []string{
					"⚠️ SSH в персональный контейнер (роль developer)",
					"--port SSH_PORT · --host HOST · --user root · --identity KEY · --print",
				}},
				{Name: "sync", Desc: []string{
					"Mutagen-синхронизация локальной папки с /workspace контейнера",
					"✏️ sync create --name N --local DIR --port P · 🔍 list · ⚠️ pause · ⚠️ resume · ✏️ flush · ✏️ terminate",
				}},
			},
		},
		{
			Title: "Служебные",
			Rows: []cli.UsageRow{
				{Name: "completion fish", Desc: []string{"🔍 скрипт автодополнения для fish"}},
				{Name: "version", Desc: []string{"🔍 версия и сборка"}},
				{Name: "help", Desc: []string{"🔍 эта справка"}},
			},
		},
	},
	Env: []cli.UsageRow{
		{Name: "AGCTL_HOME", Desc: []string{"дом данных (по умолчанию ~/.agctl)"}},
		{Name: "AGCTL_ADDR", Desc: []string{"адрес UI/API (по умолчанию 127.0.0.1:8929)"}},
		{Name: "AGCTL_PROFILE", Desc: []string{"профиль платформы (по умолчанию из config.yaml, иначе agctl)"}},
		{Name: "AGCTL_CONFIG", Desc: []string{"путь к config.yaml (по умолчанию ~/.config/agctl/config.yaml)"}},
		{Name: "AGCTL_RUNTIME", Desc: []string{"бэкенд рантаймов: docker (по умолчанию) | k8s | ax; детали — AGCTL_K8S_*, AGCTL_AX_*"}},
	},
	Footer: cli.MarksLegend + " Секреты — только через env (например AGCTL_LLM_API_KEY), в config.yaml их не пишут.",
}
