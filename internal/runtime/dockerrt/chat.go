package dockerrt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/arijjia/agctl/internal/provision"
	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// EnsureChat is the Docker adapter for the runtime-neutral ChatProvider
// contract. It binds the user's persistent DSH_HOME/work directories to one
// lazy container and returns a loopback Endpoint. A Kubernetes provider can
// instead use StorageID as a PVC key and return an in-cluster Service URL.
func (rt *Runtime) EnsureChat(ctx context.Context, spec agctlrt.ChatSpec) (agctlrt.ChatHandle, error) {
	if rt.cfg.Home == "" {
		return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: chat storage home is empty")
	}
	if spec.StorageID == "" || filepath.Base(spec.StorageID) != spec.StorageID {
		return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: invalid chat storage id %q", spec.StorageID)
	}
	base := filepath.Join(rt.cfg.Home, "chats", spec.StorageID)
	dshHome, work := filepath.Join(base, "dsh-home"), filepath.Join(base, "work")
	for _, dir := range []string{dshHome, work} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return agctlrt.ChatHandle{}, err
		}
	}
	if _, err := os.Stat(filepath.Join(dshHome, "profiles", "web", "package.json")); err != nil {
		if rt.cfg.DshSkel == nil {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: no embedded DSH skeleton for first chat boot")
		}
		if err := provision.SeedDshHome(dshHome, spec.Provider, rt.cfg.DshSkel); err != nil {
			return agctlrt.ChatHandle{}, err
		}
	}
	// Идемпотентная перезапись настроек провайдера на каждом ленивом старте:
	// DSH_HOME мог быть засеян до появления платформенного провайдера, а
	// личный секрет пользователя мог смениться между снами контейнера.
	if err := provision.RefreshProvider(dshHome, spec.Provider); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	// Брендинг персонального dsh-home: одинаков для ролей user/developer;
	// отдельные legacy dev-workspaces в v3 не создаются.
	if err := provision.EnsureChatPlugins(dshHome, rt.cfg.DshSkel); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	// Дефолт пресета синхронизируется с текущей ролью на каждом старте:
	// user получает danger-full-access, developer/admin сохраняют штатный выбор.
	if err := provision.EnsurePermissionDefaults(dshHome, spec.DefaultFullAccess); err != nil {
		return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: permission defaults: %w", err)
	}
	port, err := FreePort()
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	opts := StartOpts{HostPort: port}
	sshPort := 0
	if spec.SSHPubKey != "" {
		// Роль developer: sshd в том же контейнере (key-only, loopback хоста).
		if sshPort, err = FreePort(); err != nil {
			return agctlrt.ChatHandle{}, err
		}
		opts.SSHPubKey, opts.SSHHostPort = spec.SSHPubKey, sshPort
	}
	layout := provision.Layout{Work: work, DshHome: dshHome}
	res, err := rt.Start(ctx, layout, spec.RuntimeID, opts, spec.Provider)
	if err != nil {
		return agctlrt.ChatHandle{}, err
	}
	return agctlrt.ChatHandle{
		RuntimeID: spec.RuntimeID, InstanceID: res.ContainerID,
		Endpoint: fmt.Sprintf("http://127.0.0.1:%d", port),
		SSHPort:  res.SSHPort,
	}, nil
}

// StopChat deletes only the Docker compute container; bind-mounted chat data
// remains in $AGCTL_HOME/chats/<StorageID> for the next lazy start.
func (rt *Runtime) StopChat(ctx context.Context, handle agctlrt.ChatHandle) error {
	return rt.Stop(ctx, handle.InstanceID)
}

// LogsChat returns Docker startup logs used to mint the private DSH auth cookie.
func (rt *Runtime) LogsChat(ctx context.Context, handle agctlrt.ChatHandle, tail string) (string, error) {
	return rt.Logs(ctx, handle.InstanceID, tail)
}
