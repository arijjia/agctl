package main

// dev_hostenv.go — доступы дома из окружения хоста (localmode/hostenv.go):
// agctl dev init и up переносят пустые записи из переменных окружения, в
// которых разработчик держит токены для всех своих инструментов.

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
	"github.com/arijjia/agctl/internal/provision"
)

// devHostEnvSources — имена в agctl и их источники в окружении: встроенные
// синонимы, секреты и личные провайдеры дома, ключи доп. провайдеров
// профиля, секреты MCP-каталога, env_from профиля.
func devHostEnvSources(cfg *localmode.Config, prof *profile.Data) map[string][]string {
	var envFrom map[string][]string
	var extra []string
	if prof != nil {
		v := prof.View()
		envFrom = v.EnvFrom
		for _, p := range v.LLMExtraProviders {
			if n := strings.TrimSpace(p.APIKeyEnv); n != "" {
				extra = append(extra, n)
			}
		}
	}
	for _, s := range provision.PlatformMcpServers {
		if s.SecretEnv != "" {
			extra = append(extra, s.SecretEnv)
		}
	}
	return localmode.HostEnvSources(cfg, envFrom, extra...)
}

// devImportHostEnv переносит пустые доступы дома из окружения этого процесса
// и печатает перенесённые имена (без значений). Ошибка не останавливает
// запуск: доступы можно задать в лаунчере.
func devImportHostEnv(home string, cfg *localmode.Config) {
	prof, _ := devResolveProfile(cfg)
	got, err := localmode.ImportHostEnv(home, localmode.OpenStore(home), devHostEnvSources(cfg, prof), os.Getenv)
	if err != nil {
		fmt.Printf("  ⚠ доступы из окружения хоста: %v\n", err)
	}
	if len(got) == 0 {
		return
	}
	parts := make([]string, 0, len(got))
	for _, g := range got {
		if g.From == g.Name {
			parts = append(parts, g.Name)
		} else {
			parts = append(parts, g.Name+" ← "+g.From)
		}
	}
	fmt.Printf("✓ из окружения хоста (были пустыми в доме): %s\n", strings.Join(parts, ", "))
}

// devPrintHostEnvSources — для agctl dev secrets list: какие переменные
// окружения хоста agctl dev up переносит в пустые записи дома.
func devPrintHostEnvSources(cfg *localmode.Config, prof *profile.Data) {
	if !cfg.EnvImportOn() {
		fmt.Println("\nиз окружения хоста: выключено (config.yaml env_import: false)")
		return
	}
	sources := devHostEnvSources(cfg, prof)
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println("\nиз окружения хоста agctl dev up берёт пустые (задано в agctl — не трогает; удалить — значит снова взять из окружения):")
	for _, n := range names {
		mark := ""
		for _, src := range sources[n] {
			if strings.TrimSpace(os.Getenv(src)) != "" {
				mark = "   (в окружении сейчас: " + src + ")"
				break
			}
		}
		fmt.Printf("  %-28s ← %s%s\n", n, strings.Join(sources[n], " | "), mark)
	}
}
