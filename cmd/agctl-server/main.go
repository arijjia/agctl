// agctl-server — сервер платформы агентских рабочих мест для организации:
// много пользователей, роли, админка, рантаймы docker | k8s | ax. Ядро общее
// с локальным режимом (бинарь agctl), серверные части — модули
// server.ServerModules(), бэкенды k8s/ax — runtime_select.go.
//
//	agctl-server serve     — поднять UI + API + личные рантаймы пользователей
//	agctl-server migrate   — накатить миграции SQLite вручную
//	agctl-server version   — версия и сборка
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/arijjia/agctl/internal/cli"
	"github.com/arijjia/agctl/internal/server"
	"github.com/arijjia/agctl/internal/store"
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
	cli.SetBuild("agctl-server", version, commit, buildDate)
	if len(os.Args) < 2 {
		help.Print()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "seed-demo-users":
		cmdSeedDemoUsers(os.Args[2:])
	case "completion":
		cmdCompletion(os.Args[2:])
	case "config":
		cmdConfig(os.Args[2:])
	case "connect":
		cmdConnect(os.Args[2:])
	case "sync":
		cmdSync(os.Args[2:])
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

// cmdServe — serve сервера организации: все серверные модули и выбор
// бэкенда рантаймов (--runtime docker | k8s | ax).
func cmdServe(args []string) {
	cli.Serve(args, cli.ServeOptions{
		Modules: server.ServerModules(),
		Runtime: registerRuntimeFlags,
	})
}

// cmdMigrate — подкоманда migrate: открывает базу и применяет все
// неприменённые миграции схемы до актуального состояния.
func cmdMigrate(args []string) {
	fsSet := flag.NewFlagSet("migrate", flag.ExitOnError)
	home := fsSet.String("home", cli.DefaultHome(), "каталог данных agctl")
	_ = fsSet.Parse(args)
	absHome, err := cli.RequireHome(*home)
	if err != nil {
		log.Fatal(err)
	}
	db, err := store.Open(filepath.Join(absHome, "agctl.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	fmt.Println("migrate: схема актуальна:", filepath.Join(absHome, "agctl.db"))
}
