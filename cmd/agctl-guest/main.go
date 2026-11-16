// agctl-guest — linux-бинарь внутри образа рабочего места agctl-dsh. В образе
// ставится как /usr/local/bin/agctl, поэтому контракты `agctl bridge …`
// (скилл моста, MCP «Мост к хосту») и `agctl ax-runner` (ax-task-runner)
// не меняются.
//
//	agctl bridge list | call ИНСТРУМЕНТ k=v … | mcp — мост к хосту (локальный режим)
//	agctl ax-runner — PID 1 песочницы Google AX / Agent Substrate
//	agctl init-chat-home — init-контейнер пода k8s: DSH_HOME на томе
//	agctl version   — версия и сборка
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/arijjia/agctl/internal/cli"
)

// version, commit, buildDate подставляются ldflags при сборке (make/goreleaser).
var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

// help — команды бинаря в образе.
var help = cli.Usage{
	Tagline: "бинарь внутри образа рабочего места (agctl-guest)",
	Sections: []cli.UsageSection{{
		Title: "Команды",
		Rows: []cli.UsageRow{
			{Name: "bridge", Desc: []string{"мост к хосту (локальный режим): list | call ИНСТР k=v | mcp"}},
			{Name: "ax-runner", Desc: []string{"PID 1 песочницы Google AX / Agent Substrate"}},
			{Name: "init-chat-home", Desc: []string{"init-контейнер пода k8s: подготовить DSH_HOME на томе (--home, --refresh-provider)"}},
			{Name: "version", Desc: []string{"версия и сборка"}},
			{Name: "help", Desc: []string{"эта справка"}},
		},
	}},
}

func main() {
	log.SetFlags(log.Ltime)
	cli.SetBuild("agctl", version, commit, buildDate)
	if len(os.Args) < 2 {
		help.Print()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "bridge":
		cmdBridge(os.Args[2:])
	case "ax-runner":
		cmdAXRunner(os.Args[2:])
	case "init-chat-home": // initContainer пода k8s (k8srt), образ agctl-dsh
		cmdInitChatHome(os.Args[2:])
	case "version":
		fmt.Println(cli.VersionLine() + " guest")
	case "help", "-h", "--help":
		help.Print()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		help.Print()
		os.Exit(2)
	}
}

// cmdInitChatHome seeds/refreshes the DSH_HOME PVC from a Kubernetes init
// container. Provider data arrives only through env/Secret, never CLI args.
func cmdInitChatHome(args []string) {
	fsSet := flag.NewFlagSet("init-chat-home", flag.ExitOnError)
	home := fsSet.String("home", os.Getenv("DSH_HOME"), "каталог DSH_HOME на PVC")
	refresh := fsSet.Bool("refresh-provider", os.Getenv("AGCTL_REFRESH_PROVIDER") == "1", "обновить настройки провайдера")
	_ = fsSet.Parse(args)
	if *home == "" {
		log.Fatal("init-chat-home: требуется --home или DSH_HOME")
	}
	// Пресет разрешений роли user (danger-full-access) — как у Docker-адаптера.
	fullAccess := os.Getenv("AGCTL_DEFAULT_FULL_ACCESS") == "1"
	if err := cli.PrepareChatHome(*home, cli.ProviderFromEnv(), *refresh, fullAccess); err != nil {
		log.Fatal(err)
	}
}
