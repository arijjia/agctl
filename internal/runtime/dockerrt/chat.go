package dockerrt

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

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
	if spec.DshHome != "" {
		if !filepath.IsAbs(spec.DshHome) {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: dsh-home must be absolute: %q", spec.DshHome)
		}
		dshHome = spec.DshHome
	}
	if spec.WorkDir != "" {
		// Локальный режим: /workspace — родная папка проектов разработчика.
		if !filepath.IsAbs(spec.WorkDir) {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: work dir must be absolute: %q", spec.WorkDir)
		}
		if info, err := os.Stat(spec.WorkDir); err != nil || !info.IsDir() {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: work dir %q is not a directory", spec.WorkDir)
		}
		work = spec.WorkDir
	}
	var extraBinds []string
	if len(spec.Mounts) > 0 {
		if spec.WorkDir != "" {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: WorkDir and Mounts are mutually exclusive")
		}
		seen := map[string]bool{}
		for _, m := range spec.Mounts {
			if !filepath.IsAbs(m.Host) {
				return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: mount host path must be absolute: %q", m.Host)
			}
			if info, err := os.Stat(m.Host); err != nil || !info.IsDir() {
				return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: mount %q is not a directory", m.Host)
			}
			if !validMountName(m.Name) || seen[m.Name] {
				return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: invalid or duplicate mount name %q", m.Name)
			}
			seen[m.Name] = true
			extraBinds = append(extraBinds, m.Host+":/workspace/"+m.Name)
		}
	}
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
	// Пути как на хосте (локальный режим): папки проектов видны ещё и по
	// своим хостовым путям, процесс и воркспейс dsh по умолчанию — там же;
	// /workspace остаётся (совместимость скиллов и прежних сессий).
	var hostEnv []string
	workingDir, defaultWS := "", "/workspace"
	if spec.HostPaths {
		switch {
		case spec.WorkDir != "" && HostPathMountable(work):
			extraBinds = append(extraBinds, work+":"+work)
			workingDir, defaultWS = work, work
			hostEnv = append(hostEnv, "HOME="+work, "AGCTL_WORKSPACE_ROOT="+work)
		case len(spec.Mounts) > 0:
			var dirs []string
			for _, m := range spec.Mounts {
				if HostPathMountable(m.Host) {
					extraBinds = append(extraBinds, m.Host+":"+m.Host)
					dirs = append(dirs, m.Host)
				}
			}
			if len(dirs) > 0 {
				hostEnv = append(hostEnv, "AGCTL_WORKSPACE_DIRS="+strings.Join(dirs, ":"))
			}
		}
	}
	// Хостовый пользователь (локальный режим на Linux): файлы dsh-home,
	// оставшиеся от root, — владельцу (до любого чтения их на хосте), записи
	// passwd/group, sshd не поднимается (ему нужен root).
	if spec.User != "" {
		uid, gid, err := parseUser(spec.User)
		if err != nil {
			return agctlrt.ChatHandle{}, err
		}
		img := spec.Image
		if img == "" {
			img = rt.cfg.Image
		}
		rt.chownTree(ctx, img, dshHome, uid, gid)
		binds, err := writeUserFiles(filepath.Join(rt.cfg.Home, "chats", spec.StorageID, "etc"), uid, gid)
		if err != nil {
			return agctlrt.ChatHandle{}, fmt.Errorf("dockerrt: passwd пользователя контейнера: %w", err)
		}
		extraBinds = append(extraBinds, binds...)
		spec.SSHPubKey = ""
	}
	// Сертификаты организации (локальный режим): набор хоста — в контейнер.
	if spec.CAFile != "" {
		extraBinds = append(extraBinds, spec.CAFile+":"+ContainerCAFile+":ro")
		hostEnv = append(hostEnv, CAEnv(spec.CAFull)...)
	}
	// Воркспейс dsh по умолчанию — на хостовом пути; без него — вернуть
	// /workspace, только если по умолчанию стоял хостовый воркспейс agctl
	// (выбор владельца не трогаем). Не фатально: незнакомый формат реестра
	// (новая версия dsh) не должен закрывать место.
	var wsErr error
	if defaultWS != "/workspace" {
		_, wsErr = provision.EnsureDefaultWorkspace(dshHome, defaultWS)
	} else {
		_, wsErr = provision.ReleaseHostWorkspace(dshHome, defaultWS)
	}
	if wsErr != nil {
		log.Printf("[dockerrt] %s: воркспейс dsh по умолчанию (%s) не выставлен: %v", spec.RuntimeID, defaultWS, wsErr)
	}
	// Идемпотентная перезапись настроек провайдера на каждом ленивом старте:
	// DSH_HOME мог быть засеян до появления платформенного провайдера, а
	// личный секрет пользователя мог смениться между снами контейнера.
	if err := provision.RefreshProvider(dshHome, spec.Provider); err != nil {
		return agctlrt.ChatHandle{}, err
	}
	// Брендинг персонального dsh-home: одинаков для ролей user/developer;
	// отдельные legacy dev-workspaces в v3 не создаются.
	ensurePlugins := provision.EnsureChatPlugins
	if spec.Local {
		ensurePlugins = provision.EnsureChatPluginsLocal
	}
	if err := ensurePlugins(dshHome, rt.cfg.DshSkel); err != nil {
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
	opts := StartOpts{HostPort: port, Image: spec.Image, EgressAllowlist: spec.EgressAllowlist}
	sshPort := 0
	if spec.SSHPubKey != "" {
		// Роль developer: sshd в том же контейнере (key-only, loopback хоста).
		if sshPort, err = FreePort(); err != nil {
			return agctlrt.ChatHandle{}, err
		}
		opts.SSHPubKey, opts.SSHHostPort = spec.SSHPubKey, sshPort
	}
	layout := provision.Layout{Work: work, DshHome: dshHome}
	opts.DevEnv = spec.DevEnv // доменные креды developer'а из .env хоста
	opts.ExtraBinds = extraBinds
	opts.WorkingDir = workingDir
	opts.Env = hostEnv
	opts.User = spec.User
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

// validMountName — имя подкаталога /workspace для папки хоста.
func validMountName(name string) bool {
	if name == "" || len(name) > 64 || name == "." || name == ".." {
		return false
	}
	for i, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || (i > 0 && (r == '.' || r == '_' || r == '-'))
		if !ok {
			return false
		}
	}
	return true
}

// ContainerCAFile — путь набора сертификатов в контейнере рабочего места.
const ContainerCAFile = "/etc/agctl/ca-bundle.pem"

// CAEnv — переменные, по которым инструменты в контейнере находят набор:
// Node (dsh, npx-серверы MCP) добавляет его к своим корням всегда; полный
// набор (с корнями хоста) заменяет системный у Python (requests, httpx), uv,
// pip, curl, git и Go.
func CAEnv(full bool) []string {
	env := []string{"NODE_EXTRA_CA_CERTS=" + ContainerCAFile}
	if full {
		for _, k := range []string{"SSL_CERT_FILE", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "PIP_CERT"} {
			env = append(env, k+"="+ContainerCAFile)
		}
	}
	return env
}
