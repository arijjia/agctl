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
