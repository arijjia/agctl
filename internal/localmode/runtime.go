package localmode

import (
	"fmt"
	"strings"
)

// Рантаймы рабочего места локального режима (dev.yaml: runtime).
const (
	RuntimeDocker = "docker" // контейнер в Docker хоста (по умолчанию)
	RuntimeSbx    = "sbx"    // песочница Docker Sandboxes (microVM, CLI sbx)
)

// ParseRuntime нормализует значение рантайма: пусто — docker.
func ParseRuntime(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", RuntimeDocker:
		return RuntimeDocker, nil
	case RuntimeSbx:
		return RuntimeSbx, nil
	}
	return "", fmt.Errorf("неизвестный рантайм %q (docker | sbx)", v)
}

// RuntimeKind — выбранный рантайм; нераспознанное значение в dev.yaml
// считается docker (ошибку покажут agctl dev up и doctor).
func (c *Config) RuntimeKind() string {
	kind, err := ParseRuntime(c.Runtime)
	if err != nil {
		return RuntimeDocker
	}
	return kind
}

// SetRuntime — рантайм рабочего места в dev.yaml (выбор в лаунчере или
// флагом --runtime). Данные рабочего места от рантайма не зависят.
func SetRuntime(home, kind string) error {
	kind, err := ParseRuntime(kind)
	if err != nil {
		return err
	}
	editMu.Lock()
	defer editMu.Unlock()
	cfg, err := Load(home)
	if err != nil {
		return err
	}
	cfg.Runtime = kind
	return cfg.Save(home)
}
