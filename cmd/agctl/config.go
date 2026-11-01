package main

// config.go — пользовательские дефолты agctl в стиле webmalex-тулз
// (gitctl/tunnelctl/dutyctl): ~/.config/agctl/config.yaml. Приоритет источников:
// флаги CLI > переменные окружения (AGCTL_*) > config.yaml > встроенные дефолты.
// Путь переопределяется переменной AGCTL_CONFIG.

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// userConfig — разбираемая часть config.yaml. Поля — только дефолты для
// serve; секреты (LLM-ключи) сюда сознательно не кладём: только env/флаги.
type userConfig struct {
	Profile    string `yaml:"profile"`
	ProfileDir string `yaml:"profile_dir"`
	Serve      struct {
		Addr        string  `yaml:"addr"`
		Image       string  `yaml:"image"`
		ChatTTLMin  int     `yaml:"chat_ttl_min"`
		MemGB       float64 `yaml:"mem_gb"`
		CPUs        float64 `yaml:"cpus"`
		CORSOrigins string  `yaml:"cors_origins"`
	} `yaml:"serve"`
}

// configPath — $AGCTL_CONFIG, иначе ~/.config/agctl/config.yaml (конвенция
// ~/.config даже на macOS — как у gitctl/tunnelctl/dutyctl).
func configPath() string {
	if p := os.Getenv("AGCTL_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agctl", "config.yaml")
}

// loadUserConfig читает конфиг; отсутствующий файл — не ошибка (пустой конфиг),
// битый YAML — ошибка запуска (промолчать значило бы молча игнорировать
// настроенное владельцем).
func loadUserConfig() (userConfig, string, error) {
	var cfg userConfig
	path := configPath()
	if path == "" {
		return cfg, "", nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, path, nil
	}
	if err != nil {
		return cfg, path, fmt.Errorf("config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, path, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, path, nil
}

// cmdConfig — `agctl config`: показать путь и эффективные значения конфига
// (для проверки, что реально подхватилось перед запуском serve).
func cmdConfig(args []string) {
	cfg, path, err := loadUserConfig()
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
	fmt.Printf("    addr:          %q\n", cfg.Serve.Addr)
	fmt.Printf("    image:         %q\n", cfg.Serve.Image)
	fmt.Printf("    chat_ttl_min:  %d\n", cfg.Serve.ChatTTLMin)
	fmt.Printf("    mem_gb:        %g\n", cfg.Serve.MemGB)
	fmt.Printf("    cpus:          %g\n", cfg.Serve.CPUs)
	fmt.Printf("    cors_origins:  %q\n", cfg.Serve.CORSOrigins)
}
