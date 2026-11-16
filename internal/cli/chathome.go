package cli

// chathome.go — подготовка DSH_HOME вне Docker-адаптера: init-контейнер
// Kubernetes (agctl-server init-chat-home) и раннер AX (agctl-guest ax-runner).

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/arijjia/agctl/internal/assets"
	"github.com/arijjia/agctl/internal/provision"
)

// InitChatHome готовит DSH_HOME для init-контейнера Kubernetes: сеет скелет
// в пустой том, при refresh обновляет настройки LLM-провайдера, иначе
// оставляет существующий дом нетронутым. Возвращает флаги, что именно сделано.
func InitChatHome(home string, provider provision.Provider, refresh bool, skel fs.FS) (seeded, refreshed bool, err error) {
	if home == "" {
		return false, false, fmt.Errorf("init-chat-home: home is empty")
	}
	profile := filepath.Join(home, "profiles", "web", "package.json")
	if _, statErr := os.Stat(profile); os.IsNotExist(statErr) {
		if err := provision.SeedDshHome(home, provider, skel); err != nil {
			return false, false, err
		}
		return true, false, nil
	} else if statErr != nil {
		return false, false, statErr
	}
	if refresh {
		if err := provision.RefreshProvider(home, provider); err != nil {
			return false, false, err
		}
		return false, true, nil
	}
	return false, false, nil
}

// ProviderFromEnv — платформенный провайдер из AGCTL_LLM_* окружения
// (K8s init-контейнер, раннер AX): ключи дополнительных провайдеров — из env
// по именам api_key_env.
func ProviderFromEnv() provision.Provider {
	models := []string{}
	if raw := os.Getenv("AGCTL_LLM_MODELS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &models); err != nil {
			log.Fatalf("AGCTL_LLM_MODELS: %v", err)
		}
	}
	extras := []provision.ExtraProvider{}
	if raw := strings.TrimSpace(os.Getenv("AGCTL_LLM_EXTRA_PROVIDERS")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &extras); err != nil {
			log.Fatalf("AGCTL_LLM_EXTRA_PROVIDERS: %v", err)
		}
	}
	for i := range extras {
		env := strings.TrimSpace(extras[i].APIKeyEnv)
		if env == "" {
			env = extras[i].KeyEnv()
		}
		extras[i].APIKeyEnv = env
		extras[i].APIKey = strings.TrimSpace(os.Getenv(env))
	}
	return provision.Provider{
		Name:    os.Getenv("AGCTL_LLM_PROVIDER"),
		BaseURL: os.Getenv("AGCTL_LLM_BASE_URL"),
		Model:   os.Getenv("AGCTL_LLM_MODEL"),
		Models:  models,
		APIKey:  os.Getenv("AGCTL_LLM_API_KEY"),
		Extras:  extras,
	}
}

// PrepareChatHome — DSH_HOME готов к старту dsh: скелет (пустой том),
// провайдер (refresh), плагины платформы, дефолт пресета разрешений — тот же
// набор шагов, что у Docker-адаптера на каждом ленивом старте.
func PrepareChatHome(home string, provider provision.Provider, refresh, fullAccess bool) error {
	seeded, refreshed, err := InitChatHome(home, provider, refresh, assets.DshSkel)
	if err != nil {
		return err
	}
	if err := provision.EnsureChatPlugins(home, assets.DshSkel); err != nil {
		return err
	}
	if err := provision.EnsurePermissionDefaults(home, fullAccess); err != nil {
		return fmt.Errorf("permission defaults: %w", err)
	}
	switch {
	case seeded:
		log.Printf("chat-home: seeded %s", home)
	case refreshed:
		log.Printf("chat-home: provider refreshed in %s", home)
	default:
		log.Printf("chat-home: existing home preserved %s", home)
	}
	return nil
}
