package server

import (
	"net/http"
	"runtime"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/runtime/dockerrt"
)

// Пути как на хосте: переключатель в лаунчере пишет host_paths, работающее
// место ждёт перезапуска, после него рантайм получает HostPaths, а вью места
// показывает папку по её хостовому пути (не /workspace).
func TestLocalHostPathsToggle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("на Windows режим недоступен")
	}
	env, h := newLocalUIEnv(t)
	c := loginOwner(t, env)
	owner := env.srv.local.ownerID
	if code, body := env.do(c, "GET", "/api/local/ui", nil); code != 200 || !strings.Contains(body, `"hostPaths":false`) || !strings.Contains(body, `"hostPathsAvailable":true`) {
		t.Fatalf("состояние до: %d %s", code, body)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatal("старт")
	}
	env.waitTask("idle")
	if spec := env.rt.chatSpecs[env.srv.chatRuntimeID(owner, 0)]; spec.HostPaths {
		t.Fatal("HostPaths до включения")
	}
	if code, body := env.do(c, "PUT", "/api/local/ui/hostpaths", map[string]any{"enabled": true}); code != 200 {
		t.Fatalf("включение: %d %s", code, body)
	}
	if !h.HostPaths() {
		t.Fatal("host_paths не записан")
	}
	if snap := env.srv.local.task.snapshot(); snap["restartNeeded"] != true {
		t.Fatalf("работающее место должно ждать перезапуска: %v", snap)
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/restart", nil); code != http.StatusAccepted {
		t.Fatal("restart")
	}
	env.waitTask("idle")
	if spec := env.rt.chatSpecs[env.srv.chatRuntimeID(owner, 0)]; !spec.HostPaths {
		t.Fatal("рантайм не получил HostPaths")
	}
	wd := env.srv.localWorkDir()
	want := "/workspace"
	if dockerrt.HostPathMountable(wd) {
		want = wd
	}
	code, body := env.do(c, "GET", "/api/local/ui/places", nil)
	if code != 200 || !strings.Contains(body, `"container":"`+want+`"`) {
		t.Fatalf("вью места: папка %s должна быть видна как %s: %s", wd, want, body)
	}
}
