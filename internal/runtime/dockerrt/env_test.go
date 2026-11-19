package dockerrt

import (
	"strings"
	"testing"
)

// Ключи провайдеров LLM не попадают в env контейнера: dsh берёт их из
// DSH_HOME/.credentials.yaml (карточка local-llm-key-container-env).
func TestContainerEnvHasNoLLMKeys(t *testing.T) {
	env := containerEnv("ws1", []string{"GITLAB_TOKEN=gl"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "AGCTL_LLM_API_KEY") {
			t.Fatalf("ключ LLM в env контейнера: %q", kv)
		}
	}
	joined := strings.Join(env, "\n")
	for _, want := range []string{"AGCTL_WS_ID=ws1", "HOME=/workspace", "GITLAB_TOKEN=gl"} {
		if !strings.Contains(joined, want) {
			t.Errorf("нет %q в env: %v", want, env)
		}
	}
}

// Имя подкаталога /workspace для папки хоста: без путей и ведущих точек.
func TestValidMountName(t *testing.T) {
	for _, ok := range []string{"app", "lib-2", "a.b_c"} {
		if !validMountName(ok) {
			t.Errorf("%q должно быть допустимо", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "../x", "a/b", "-x", ".hidden", "Upper", strings.Repeat("a", 65)} {
		if validMountName(bad) {
			t.Errorf("%q должно быть отклонено", bad)
		}
	}
}
