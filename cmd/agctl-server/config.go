package main

// config.go — пользовательские дефолты agctl в стиле webmalex-тулз
// (gitctl/tunnelctl/dutyctl): ~/.config/agctl/config.yaml. Приоритет источников:
// флаги CLI > переменные окружения (AGCTL_*) > config.yaml > встроенные дефолты.
// Путь переопределяется переменной AGCTL_CONFIG.

import (
	"fmt"
	"os"

	"github.com/arijjia/agctl/internal/cli"
)

// cmdConfig — `agctl config`: показать путь и эффективные значения конфига
// (для проверки, что реально подхватилось перед запуском serve).
func cmdConfig(args []string) {
	cfg, path, err := cli.LoadUserConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "agctl config: %v\n", err)
		os.Exit(1)
	}
	if path == "" {
		fmt.Println("конфиг: не определён (нет домашнего каталога)")
		return
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		fmt.Printf("конфиг: %s (не найден — используются env и встроенные дефолты)\n", path)
		return
	}
	fmt.Printf("конфиг: %s\n", path)
	fmt.Printf("  profile:     %q\n", cfg.Profile)
	fmt.Printf("  profile_dir: %q\n", cfg.ProfileDir)
	fmt.Printf("  serve:\n")
	fmt.Printf("    runtime:       %q\n", cfg.Serve.Runtime)
	fmt.Printf("    addr:          %q\n", cfg.Serve.Addr)
	fmt.Printf("    image:         %q\n", cfg.Serve.Image)
	fmt.Printf("    chat_ttl_min:  %d\n", cfg.Serve.ChatTTLMin)
	fmt.Printf("    register_per_min: %d\n", cfg.Serve.RegisterPerMin)
	fmt.Printf("    mem_gb:        %g\n", cfg.Serve.MemGB)
	fmt.Printf("    cpus:          %g\n", cfg.Serve.CPUs)
	fmt.Printf("    cors_origins:  %q\n", cfg.Serve.CORSOrigins)
}
