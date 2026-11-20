package dockerrt

import (
	"slices"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/system"
)

func TestApplyHardeningDefault(t *testing.T) {
	rt := &Runtime{cfg: Config{}}
	hc := &container.HostConfig{}
	rt.applyHardening(hc)
	if !slices.Equal(hc.CapDrop, []string{"ALL"}) {
		t.Fatalf("CapDrop = %v", hc.CapDrop)
	}
	for _, c := range []string{"SETUID", "SETGID", "SYS_CHROOT", "CHOWN", "DAC_OVERRIDE"} {
		if !slices.Contains(hc.CapAdd, c) {
			t.Errorf("нет %s в CapAdd (нужен entrypoint/sshd): %v", c, hc.CapAdd)
		}
	}
	for _, c := range []string{"NET_RAW", "MKNOD", "SYS_ADMIN", "NET_ADMIN", "SETFCAP"} {
		if slices.Contains(hc.CapAdd, c) {
			t.Errorf("%s не должен возвращаться: %v", c, hc.CapAdd)
		}
	}
	if !slices.Contains(hc.SecurityOpt, "no-new-privileges:true") {
		t.Errorf("SecurityOpt = %v", hc.SecurityOpt)
	}
	if hc.Resources.PidsLimit == nil || *hc.Resources.PidsLimit != defaultPidsLimit {
		t.Errorf("PidsLimit = %v", hc.Resources.PidsLimit)
	}
	if hc.Runtime != "" {
		t.Errorf("Runtime по умолчанию должен быть пустым (runc демона): %q", hc.Runtime)
	}
}

func TestApplyHardeningRuntimeAndLimit(t *testing.T) {
	rt := &Runtime{cfg: Config{ContainerRuntime: "runsc", PidsLimit: 512}}
	hc := &container.HostConfig{}
	rt.applyHardening(hc)
	if hc.Runtime != "runsc" {
		t.Errorf("Runtime = %q", hc.Runtime)
	}
	if *hc.Resources.PidsLimit != 512 {
		t.Errorf("PidsLimit = %d", *hc.Resources.PidsLimit)
	}
}

func TestApplyHardeningOff(t *testing.T) {
	rt := &Runtime{cfg: Config{NoHardening: true, ContainerRuntime: "kata"}}
	hc := &container.HostConfig{}
	rt.applyHardening(hc)
	if hc.CapDrop != nil || hc.CapAdd != nil || hc.SecurityOpt != nil || hc.Resources.PidsLimit != nil {
		t.Errorf("при NoHardening упрочнение не применяется: %+v", hc)
	}
	if hc.Runtime != "kata" {
		t.Errorf("рантайм ставится и без упрочнения: %q", hc.Runtime)
	}
}

func TestCheckContainerRuntime(t *testing.T) {
	rts := map[string]system.RuntimeWithStatus{"runc": {}, "io.containerd.runc.v2": {}}
	if err := checkContainerRuntime("runc", rts); err != nil {
		t.Fatal(err)
	}
	err := checkContainerRuntime("runsc", rts)
	if err == nil || !strings.Contains(err.Error(), "runsc") || !strings.Contains(err.Error(), "runc") {
		t.Fatalf("ожидалась ошибка со списком рантаймов: %v", err)
	}
}
