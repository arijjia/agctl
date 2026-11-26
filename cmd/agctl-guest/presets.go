package main

// presets.go — `agctl presets-sync`: перед стартом dsh собирает патч-слой
// управляемых пресетов agctl ($DSH_HOME/.agent-presets/agctl-*) и печатает
// путь файла (пусто — пресетов нет). Вызывается entrypoint образа, результат
// уходит в `dsh web --patch`. Общая команда обоих вариантов guest: пресеты
// воркспейсов есть и в серверном режиме.

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/arijjia/agctl/internal/provision"
)

// standardPresetPath — состав штатного пресета standard установленного dsh.
const standardPresetPath = "/usr/local/lib/node_modules/@deepseek-ai/dsh/node_modules/@deepseek-ai/dsh-web-app/presets/standard.patch.yml"

func cmdPresetsSync(args []string) {
	fs := flag.NewFlagSet("presets-sync", flag.ExitOnError)
	home := fs.String("dsh-home", os.Getenv("DSH_HOME"), "DSH_HOME рабочего места")
	base := fs.String("standard", standardPresetPath, "patch-файл штатного пресета standard")
	_ = fs.Parse(args)
	if *home == "" {
		log.Fatal("presets-sync: не задан DSH_HOME")
	}
	raw, err := os.ReadFile(*base) // #nosec G304 — путь из образа или флага оператора
	if err != nil {
		log.Fatalf("presets-sync: штатный пресет: %v", err)
	}
	path, n, err := provision.SyncPresetPatch(*home, *home, raw)
	if err != nil {
		log.Fatalf("presets-sync: %v", err)
	}
	if n > 0 {
		log.Printf("presets-sync: пресетов agctl: %d → %s", n, path)
		fmt.Println(path)
	}
}
