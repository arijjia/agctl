// agctl-guest — linux-бинарь внутри образа рабочего места. Собирается в двух
// вариантах из одного кода (build-тег server), каждый под свой режим и образ:
//
//	локальный (по умолчанию, тегов нет)  → образ agctl-dsh*: только `bridge`
//	  — мост к хосту (локальный режим agctl dev);
//	серверный (-tags server)             → образ agctl-dsh-server*: `ax-runner`
//	  (PID 1 песочницы Google AX / Agent Substrate) и `init-chat-home`
//	  (init-контейнер пода k8s: DSH_HOME на томе).
//
// В образе бинарь ставится как /usr/local/bin/agctl, поэтому контракты вызовов
// (`agctl bridge …`, `agctl ax-runner …`) не меняются.
//
//	agctl bridge list | call ИНСТРУМЕНТ k=v … | mcp — мост к хосту (локальный)
//	agctl presets-sync — патч пресетов agctl для dsh (оба варианта, entrypoint)
//	agctl ax-runner — PID 1 песочницы AX / Agent Substrate (серверный)
//	agctl init-chat-home — init-контейнер пода k8s (серверный)
//	agctl version   — версия, сборка и вариант (guest local | guest server)
package main

import (
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

// help — команды бинаря в образе; modeRows() добавляет команды варианта.
var help = cli.Usage{
	Tagline: "бинарь внутри образа рабочего места (agctl-guest " + guestVariant + ")",
	Sections: []cli.UsageSection{{
		Title: "Команды",
		Rows: append(append(modeRows(),
			cli.UsageRow{Name: "presets-sync", Desc: []string{"собрать патч пресетов agctl для dsh (entrypoint образа)"}},
			cli.UsageRow{Name: "prepare", Desc: []string{"метка версий dsh-home, предупреждение об откате dsh (entrypoint образа)"}}),
			cli.UsageRow{Name: "version", Desc: []string{"версия и сборка"}},
			cli.UsageRow{Name: "help", Desc: []string{"эта справка"}},
		),
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
	case "presets-sync":
		cmdPresetsSync(os.Args[2:])
	case "prepare":
		cmdPrepare(os.Args[2:])
	case "ax-runner":
		cmdAXRunner(os.Args[2:])
	case "init-chat-home": // initContainer пода k8s (k8srt), серверный образ
		cmdInitChatHome(os.Args[2:])
	case "version":
		fmt.Println(cli.VersionLine() + " guest " + guestVariant)
	case "help", "-h", "--help":
		help.Print()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		help.Print()
		os.Exit(2)
	}
}
