//go:build server

package main

// initchathome.go — `agctl init-chat-home`: seeds/refreshes the DSH_HOME PVC
// from a Kubernetes init container (k8srt, контейнер seed-dsh-home). Provider
// data arrives only through env/Secret, never CLI args.

import (
	"flag"
	"log"
	"os"

	"github.com/arijjia/agctl/internal/cli"
)

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
