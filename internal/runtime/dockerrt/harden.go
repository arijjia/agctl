package dockerrt

import (
	"fmt"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
)

// Упрочнение контейнера рабочего места (карточка dockerrt-hardening-runtime).
//
// Entrypoint работает от root: chown/chmod каталогов на bind-маунтах хоста,
// sshd с privsep (chroot, смена uid/gid). Поэтому non-root не включаем, а
// оставляем root с минимальным набором capabilities — тем же, что у k8srt
// с SSH, плюс FSETID (сохранение setgid-битов при chmod/копировании).
// Сняты относительно дефолта Docker: MKNOD, NET_RAW, NET_BIND_SERVICE,
// SETFCAP, SETPCAP. Seccomp — дефолтный профиль демона (не трогаем).

// hardenedCaps — capabilities, которые остаются после CapDrop ALL.
var hardenedCaps = []string{
	"CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID",
	"SETGID", "SETUID", "SYS_CHROOT", "KILL", "AUDIT_WRITE",
}

// defaultPidsLimit — потолок процессов: сборки (make -j, npm, go test)
// укладываются с запасом, fork-бомба — нет.
const defaultPidsLimit int64 = 4096

// applyHardening дописывает в HostConfig упрочнение и OCI-рантайм.
func (rt *Runtime) applyHardening(hc *container.HostConfig) {
	if rt.cfg.ContainerRuntime != "" {
		hc.Runtime = rt.cfg.ContainerRuntime
	}
	if rt.cfg.NoHardening {
		return
	}
	hc.CapDrop = []string{"ALL"}
	hc.CapAdd = append([]string(nil), hardenedCaps...)
	hc.SecurityOpt = append(hc.SecurityOpt, "no-new-privileges:true")
	limit := rt.cfg.PidsLimit
	if limit <= 0 {
		limit = defaultPidsLimit
	}
	hc.Resources.PidsLimit = &limit
}

// checkContainerRuntime — рантайм зарегистрирован в демоне (docker info),
// иначе понятная ошибка вместо отказа на первом старте контейнера.
func checkContainerRuntime(name string, runtimes map[string]system.RuntimeWithStatus) error {
	if _, ok := runtimes[name]; ok {
		return nil
	}
	have := make([]string, 0, len(runtimes))
	for k := range runtimes {
		have = append(have, k)
	}
	sort.Strings(have)
	return fmt.Errorf("docker: рантайм %q не зарегистрирован в демоне (есть: %s); установите его (gVisor: runsc install, Kata: kata-runtime) и перезапустите dockerd", name, strings.Join(have, ", "))
}
