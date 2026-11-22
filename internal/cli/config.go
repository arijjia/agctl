package cli

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

// UserConfig — разбираемая часть config.yaml. Поля — только дефолты для
// serve; секреты (LLM-ключи) сюда сознательно не кладём: только env/флаги.
type UserConfig struct {
	Profile    string `yaml:"profile"`
	ProfileDir string `yaml:"profile_dir"`
	Serve      struct {
		Runtime          string `yaml:"runtime"` // docker | k8s | ax
		Addr             string `yaml:"addr"`
		PublicURL        string `yaml:"public_url"`
		Image            string `yaml:"image"`
		DevImages        string `yaml:"dev_images"`
		DevEnv           string `yaml:"dev_env"`
		LauncherSlotsMax int    `yaml:"launcher_slots_max"` // 0 = дефолт 3 (карточка developer-multi-containers)
		// HA leader-election (карточка ha-control-plane): Lease для выбора
		// лидера фоновых циклов при 2+ репликах шлюза.
		HALeaderLease      string    `yaml:"ha_leader_lease"`
		HALeaderKubeconfig string    `yaml:"ha_leader_kubeconfig"`
		HALeaderNamespace  string    `yaml:"ha_leader_namespace"`
		StoreDSN           string    `yaml:"store_dsn"` // postgres://… — PG-бэкенд store (карточка ha-control-plane); пусто = SQLite-файл
		ChatTTLMin         int       `yaml:"chat_ttl_min"`
		RegisterPerMin     int       `yaml:"register_per_min"`
		MemGB              float64   `yaml:"mem_gb"`
		CPUs               float64   `yaml:"cpus"`
		CORSOrigins        string    `yaml:"cors_origins"`
		EgressAllowlist    bool      `yaml:"egress_allowlist"` // карточки egress-allowlist, dockerrt-egress-flag-key-proxy
		EgressAllow        string    `yaml:"egress_allow"`     // хосты/IPv4/CIDR через запятую сверх профиля
		K8s                K8sConfig `yaml:"k8s"`
		AX                 AXConfig  `yaml:"ax"`
	} `yaml:"serve"`
}

// ConfigPath — $AGCTL_CONFIG, иначе ~/.config/agctl/config.yaml (конвенция
// ~/.config даже на macOS — как у gitctl/tunnelctl/dutyctl).
func ConfigPath() string {
	if p := os.Getenv("AGCTL_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "agctl", "config.yaml")
}

// LoadUserConfig читает конфиг; отсутствующий файл — не ошибка (пустой конфиг),
// битый YAML — ошибка запуска (промолчать значило бы молча игнорировать
// настроенное владельцем).
func LoadUserConfig() (UserConfig, string, error) {
	var cfg UserConfig
	path := ConfigPath()
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

// K8sConfig — дефолты бэкенда k8s (serve.k8s.*; env AGCTL_K8S_* и флаги
// --k8s-* перекрывают).
type K8sConfig struct {
	Namespace       string `yaml:"namespace"`
	Kubeconfig      string `yaml:"kubeconfig"`
	Image           string `yaml:"image"`
	StorageClass    string `yaml:"storage_class"`
	StorageSize     string `yaml:"storage_size"`
	RuntimeClass    string `yaml:"runtime_class"`
	SharedClaim     string `yaml:"shared_claim"`
	SSHHost         string `yaml:"ssh_host"`
	ReadyTimeoutSec int    `yaml:"ready_timeout_sec"`
	GatewaySelector string `yaml:"gateway_selector"`
	PullPolicy      string `yaml:"pull_policy"`
	// EgressPolicy — "1" = egress-политика чат-подов (DNS + ipBlock
	// LLM-хостов; карточка k8s-pod-egress-policy). Строка для согласованности
	// с булевыми полями других секций serve.
	EgressPolicy string `yaml:"egress_policy"`
}

// AXConfig — дефолты бэкенда ax (serve.ax.*; env AGCTL_AX_* и флаги --ax-*
// перекрывают).
type AXConfig struct {
	Server          string `yaml:"server"`
	Atespace        string `yaml:"atespace"`
	Image           string `yaml:"image"`
	RouterURL       string `yaml:"router_url"`
	RouterCA        string `yaml:"router_ca"`
	RouterCert      string `yaml:"router_cert"`
	RouterKey       string `yaml:"router_key"`
	ReadyTimeoutSec int    `yaml:"ready_timeout_sec"`
	// AteAPI — адрес ate-api-server (Control API Substrate) для автоматической
	// egress-инъекции ключей LLM при ax-inject-llm-key=0; пусто = политику
	// акторов ведёт оператор вручную.
	AteAPI string `yaml:"ate_api"`
	// AteCluster — имя кластера в credentialUri ate-secret://k8s.io/cluster/<cluster>/….
	AteCluster string `yaml:"ate_cluster"`
	// DebugSSH — spec.debug задач ax (guest-сервисы ax ssh); строка "1"/"0".
	DebugSSH string `yaml:"debug_ssh"`
}
