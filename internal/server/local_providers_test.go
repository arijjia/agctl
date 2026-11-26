package server

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/arijjia/agctl/internal/localmode"
)

// providerHooks — fakeHooks + личные провайдеры в памяти.
type providerHooks struct {
	*fakeHooks
	pmu       sync.Mutex
	providers []localmode.Provider
	keys      map[string]string
}

func (h *providerHooks) Providers() []localmode.Provider {
	h.pmu.Lock()
	defer h.pmu.Unlock()
	return append([]localmode.Provider(nil), h.providers...)
}
func (h *providerHooks) SaveProvider(p localmode.Provider, key *string) error {
	h.pmu.Lock()
	defer h.pmu.Unlock()
	replaced := false
	for i := range h.providers {
		if h.providers[i].Name == p.Name {
			h.providers[i], replaced = p, true
		}
	}
	if !replaced {
		h.providers = append(h.providers, p)
	}
	if key != nil {
		h.keys[localmode.ProviderKeyEnv(p.Name)] = *key
	}
	return nil
}
func (h *providerHooks) DeleteProvider(name string) error {
	h.pmu.Lock()
	defer h.pmu.Unlock()
	kept := h.providers[:0]
	for _, p := range h.providers {
		if p.Name != name {
			kept = append(kept, p)
		}
	}
	h.providers = kept
	delete(h.keys, localmode.ProviderKeyEnv(name))
	return nil
}
func (h *providerHooks) ModelKey(env string) string {
	h.pmu.Lock()
	defer h.pmu.Unlock()
	return h.keys[env]
}

// Личный провайдер добавляется из лаунчера под своим именем, виден в
// провайдере сервера (значит, попадёт в харнесс при старте места) вместе с
// ключом, а имя провайдера организации занять нельзя.
func TestLocalProvidersCRUD(t *testing.T) {
	env, base := newLocalUIEnv(t)
	h := &providerHooks{fakeHooks: base, keys: map[string]string{}}
	env.srv.cfg.Local.Hooks = h
	c := loginOwner(t, env)

	code, body := env.do(c, "POST", "/api/local/ui/providers", map[string]any{
		"name": "zai", "baseURL": "https://api.z.ai/api/coding/paas/v4/", "models": []string{"glm-5.3", "glm-5.3-flash"}, "key": "unit-zai-key",
	})
	if code != http.StatusCreated || !strings.Contains(body, `"name":"zai"`) || !strings.Contains(body, `"source":"own"`) {
		t.Fatalf("создание: %d %s", code, body)
	}
	if strings.Contains(body, "unit-zai-key") {
		t.Fatal("ключ утёк в ответ API")
	}
	var found bool
	for _, e := range env.srv.defaultLLM().Extras {
		if e.Alias() == "zai" {
			found = true
			if e.APIKey != "unit-zai-key" || e.BaseURL != "https://api.z.ai/api/coding/paas/v4" || e.DefaultModel() != "glm-5.3" {
				t.Fatalf("провайдер сервера: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("личный провайдер не попал в провайдер сервера")
	}
	if code, _ := env.do(c, "POST", "/api/local/ui/providers", map[string]any{"name": "zai", "baseURL": "https://x.example/v1", "models": []string{"m"}}); code != http.StatusConflict {
		t.Fatalf("повторное имя: %d", code)
	}
	if code, body := env.do(c, "POST", "/api/local/ui/providers", map[string]any{"name": "Плохое имя", "baseURL": "https://x.example/v1", "models": []string{"m"}}); code != http.StatusBadRequest {
		t.Fatalf("некорректное имя: %d %s", code, body)
	}
	// Обновление без поля key ключ не трогает.
	code, body = env.do(c, "PUT", "/api/local/ui/providers/zai", map[string]any{"baseURL": "https://api.z.ai/v4", "models": []string{"glm-5.3"}})
	if code != http.StatusOK || !strings.Contains(body, `"keySet":true`) {
		t.Fatalf("обновление: %d %s", code, body)
	}
	if code, _ := env.do(c, "DELETE", "/api/local/ui/providers/zai", nil); code != http.StatusOK {
		t.Fatalf("удаление: %d", code)
	}
	for _, e := range env.srv.defaultLLM().Extras {
		if e.Alias() == "zai" {
			t.Fatal("удалённый провайдер остался в провайдере сервера")
		}
	}
}
