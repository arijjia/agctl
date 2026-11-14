package server

// dev_env_test.go — проброс доменных переменных хоста в контейнер
// developer'а (ChatSpec.DevEnv → docker Env): имена берутся из конфига
// serve (Config.DevEnv), значения — из окружения процесса serve (в него
// .env экспортирует make).

import (
	"strings"
	"testing"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

func TestDevEnvInjectedForDeveloperOnly(t *testing.T) {
	t.Setenv("WSCHECK_GITLAB_TOKEN", "glpat-test")
	t.Setenv("WSCHECK_EMPTY", "")

	env := newTestEnvWith(t, 2, func(c *Config) {
		c.DevEnv = []string{"WSCHECK_GITLAB_TOKEN", "WSCHECK_EMPTY", "WSCHECK_UNSET"}
	})
	env.register(env.admin, "anna", "secret1") // admin = u1
	user := env.newClient()
	env.register(user, "boris", "secret2")        // user = u2
	dev := makeDeveloper(env, "dev1", "devpass1") // developer = u3

	// developer: runtime поднимается с доменными переменными
	if code, _ := env.do(dev, "POST", "/api/me/runtime/start", nil); code != 200 {
		t.Fatalf("developer start: %d", code)
	}
	specDev, okDev := devSpecFor(env, "-u3")
	if !okDev {
		t.Fatal("ChatSpec developer не зафиксирован")
	}
	// + путь к файлу доступов для команд агента (agent_creds.go)
	if len(specDev.DevEnv) != 2 || !strings.Contains(specDev.DevEnv[0], "WSCHECK_GITLAB_TOKEN=glpat-test") ||
		specDev.DevEnv[1] != "AGCTL_CREDENTIALS_ENV=/home/dsh/.agctl/credentials.env" {
		t.Fatalf("DevEnv developer: %v (незаданные имена и пустые значения пропускаются)", specDev.DevEnv)
	}

	// user: доменного окружения нет
	if code, _ := env.do(user, "POST", "/api/me/runtime/start", nil); code != 403 {
		// user-роль стартует лениво через chatProxy; прямого lifecycle у неё нет
		_ = code
	}
	if _, err := env.srv.ensureChat(2); err != nil {
		t.Fatalf("ensureChat user: %v", err)
	}
	specUser, okUser := devSpecFor(env, "-u2")
	if !okUser {
		t.Fatal("ChatSpec user не зафиксирован")
	}
	if len(specUser.DevEnv) != 0 {
		t.Fatalf("DevEnv user должен быть пуст: %v", specUser.DevEnv)
	}
}

// devSpecFor — ChatSpec по суффиксу RuntimeID ("-u<id>"; полный id содержит
// хэш дома: agctl-chat-<hash>-u<id>).
func devSpecFor(env *testEnv, suffix string) (agctlrt.ChatSpec, bool) {
	env.srv.chatMu.Lock()
	defer env.srv.chatMu.Unlock()
	for rid, spec := range env.rt.chatSpecs {
		if strings.HasSuffix(rid, suffix) {
			return spec, true
		}
	}
	return agctlrt.ChatSpec{}, false
}
