package server

import (
	"net/http"
	"testing"
)

// caHooks — хост лаунчера с набором сертификатов организации.
type caHooks struct {
	*fakeHooks
	path string
	full bool
}

func (h caHooks) CABundle() (string, bool, error) { return h.path, h.full, nil }

// Набор сертификатов хоста уходит в спецификацию рабочего места.
func TestLocalCABundleInSpec(t *testing.T) {
	env, h := newLocalUIEnv(t)
	env.srv.cfg.Local.Hooks = caHooks{fakeHooks: h, path: "/host/run/ca-bundle.pem", full: true}
	c := loginOwner(t, env)
	if code, _ := env.do(c, "POST", "/api/local/ui/start", nil); code != http.StatusAccepted {
		t.Fatal("старт основного")
	}
	env.waitTask("idle")
	spec := env.rt.chatSpecs[env.srv.chatRuntimeID(env.srv.local.ownerID, 0)]
	if spec.CAFile != "/host/run/ca-bundle.pem" || !spec.CAFull {
		t.Fatalf("спецификация: CAFile=%q CAFull=%v", spec.CAFile, spec.CAFull)
	}
}
