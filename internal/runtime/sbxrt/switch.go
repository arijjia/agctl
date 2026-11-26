package sbxrt

import (
	"context"
	"strings"
	"sync"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// Kind — идентификаторы рантаймов рабочего места локального режима.
const (
	KindDocker = "docker"
	KindSbx    = "sbx"
)

// Switch — ChatProvider локального режима с выбором рантайма: новое рабочее
// место поднимается в рантайме, который сейчас возвращает Kind (dev.yaml
// перечитывается на каждый старт, так что смена в лаунчере действует со
// следующего перезапуска места), а уже работающее обслуживает тот рантайм,
// который его создал (по префиксу InstanceID).
type Switch struct {
	Docker agctlrt.ChatProvider
	Kind   func() string
	// NewSbx создаёт рантайм sbx при первом обращении: пока выбран docker,
	// наличие sbx на машине не требуется.
	NewSbx func(ctx context.Context) (*Runtime, error)

	mu  sync.Mutex
	sbx *Runtime
}

func (s *Switch) sandboxes(ctx context.Context) (*Runtime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sbx == nil {
		rt, err := s.NewSbx(ctx)
		if err != nil {
			return nil, err
		}
		s.sbx = rt
	}
	return s.sbx, nil
}

func isSbx(h agctlrt.ChatHandle) bool { return strings.HasPrefix(h.InstanceID, InstancePrefix) }

func (s *Switch) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	if s.Kind() != KindSbx {
		return s.Docker.EnsureChat(ctx, spec)
	}
	rt, err := s.sandboxes(ctx)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	return rt.EnsureChat(ctx, spec)
}

func (s *Switch) StopChat(ctx context.Context, h agctlrt.ChatHandle) error {
	if !isSbx(h) {
		return s.Docker.StopChat(ctx, h)
	}
	rt, err := s.sandboxes(ctx)
	if err != nil {
		return err
	}
	return rt.StopChat(ctx, h)
}

func (s *Switch) LogsChat(ctx context.Context, h agctlrt.ChatHandle, tail string) (string, error) {
	if !isSbx(h) {
		return s.Docker.LogsChat(ctx, h, tail)
	}
	rt, err := s.sandboxes(ctx)
	if err != nil {
		return "", err
	}
	return rt.LogsChat(ctx, h, tail)
}

func (s *Switch) HasActiveSSH(ctx context.Context, h agctlrt.ChatHandle) (bool, error) {
	if isSbx(h) {
		return false, nil
	}
	return s.Docker.HasActiveSSH(ctx, h)
}

// HealthStatus — здоровье инстанса для сверки сервера (healthReporter).
func (s *Switch) HealthStatus(ctx context.Context, instanceID string) (string, error) {
	if strings.HasPrefix(instanceID, InstancePrefix) {
		rt, err := s.sandboxes(ctx)
		if err != nil {
			return "", err
		}
		return rt.HealthStatus(ctx, instanceID)
	}
	if hr, ok := s.Docker.(interface {
		HealthStatus(ctx context.Context, containerID string) (string, error)
	}); ok {
		return hr.HealthStatus(ctx, instanceID)
	}
	return "", nil
}

// Close завершает процессы dsh web песочниц.
func (s *Switch) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sbx != nil {
		return s.sbx.Close()
	}
	return nil
}
