// agctl — бинарь разработчика: платформа агентских рабочих мест на своей
// машине (локальный режим). Ядро общее с сервером организации (бинарь
// agctl-server), но серверные модули, админка и бэкенды k8s/ax сюда не
// входят: только Docker на этой машине.
//
//	agctl dev init | up | down | status | open | logs | secrets | bridge | update | rollback | doctor
//	agctl backlog … — CLI ведения бэклогов (формат v1, gitctl-совместимый)
//	agctl version   — версия и сборка
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/arijjia/agctl/internal/backlog"
	"github.com/arijjia/agctl/internal/cli"
)

// version, commit, buildDate подставляются ldflags при сборке (make/goreleaser).
var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

// main — диспетчер подкоманд. Неизвестная команда печатает справку и
// завершает процесс кодом 2.
func main() {
	log.SetFlags(log.Ltime)
	cli.SetBuild("agctl", version, commit, buildDate)
	if len(os.Args) < 2 {
		help.Print()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dev": // локальный режим: платформа на машине разработчика
		cmdDev(os.Args[2:])
	case "backlog":
		os.Exit(backlog.Main(os.Args[2:], os.Stdout, os.Stderr))
	case "completion":
		cmdCompletion(os.Args[2:])
	case "version":
		fmt.Println(cli.VersionLine())
	case "help", "-h", "--help":
		help.Print()
	default:
		fmt.Fprintf(os.Stderr, "неизвестная команда %q\n\n", os.Args[1])
		help.Print()
		os.Exit(2)
	}
}
