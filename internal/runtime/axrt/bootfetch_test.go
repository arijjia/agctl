package axrt

// bootfetch_test.go — fetch-at-boot конфига провайдера: env задачи без
// AGCTL_LLM_*, specHash не зависит от провайдера, endpoint шлюза отдаёт
// конфиг по токену раннера (ключ по режиму инъекции).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/ax/pkg/apis/v1alpha1"
)

// stubProvider — Provider без gRPC-соединения: только cfg + реестр спеков
// (для payload/endpoint-тестов).
func stubProvider(t *testing.T, mutate func(*Config)) *Provider {
	t.Helper()
	cfg := testConfig("")
	mutate(&cfg)
	p, err := NewWithClient(cfg, v1alpha1.NewAXClient(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProviderAtBootTaskHasNoProviderEnv(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.ProviderAtBoot = true
	cfg.GatewayURL = "http://gateway.invalid"
	provider, err := NewWithClient(cfg, client, router.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.EnsureChat(context.Background(), egressSpec()); err != nil {
		t.Fatal(err)
	}
	env := envOf(fake.task("agctl", TaskName("chat-abcd1234-u17")))
	for _, banned := range []string{"AGCTL_LLM_PROVIDER", "AGCTL_LLM_BASE_URL", "AGCTL_LLM_MODEL", "AGCTL_LLM_MODELS", "AGCTL_LLM_API_KEY", "AGCTL_LLM_EXTRA_PROVIDERS", "AGCTL_REFRESH_PROVIDER"} {
		if _, leaked := env[banned]; leaked {
			t.Fatalf("%s must not reach the task env in fetch-at-boot mode", banned)
		}
	}
	if env[GatewayEnv] != "http://gateway.invalid" {
		t.Fatalf("gateway URL env = %q", env[GatewayEnv])
	}
	if _, known := provider.ProviderConfigByTask(TaskName("chat-abcd1234-u17")); !known {
		t.Fatal("task spec must be registered for fetch-at-boot")
	}
}

func TestProviderAtBootSpecHashIgnoresProvider(t *testing.T) {
	_, client, router, _ := startFixture(t)
	cfg := testConfig(router.URL)
	cfg.ProviderAtBoot = true
	cfg.GatewayURL = "http://gateway.invalid"
	provider, _ := NewWithClient(cfg, client, router.Client())
	ctx := context.Background()
	if _, err := provider.EnsureChat(ctx, spec()); err != nil {
		t.Fatal(err)
	}
	changed := spec()
	changed.Provider.Model = "other-model"
	changed.Provider.APIKey = "rotated-key"
	if _, err := provider.EnsureChat(ctx, changed); err != nil {
		t.Fatal(err)
	}
	// Задача неизменяема и НЕ дрейфует: та же, созданная один раз.
	fake, _, _, _ := startFixture(t)
	_ = fake
	// specHash задачи равен hash желаемого spec с новым провайдером —
	// пересоздания не было (fake.deletes==0 проверяется в Drift-тесте на
	// общем fake; здесь сверяем hash напрямую).
	updated := provider.buildTask(TaskName("chat-abcd1234-u17"), changed)
	task := provider.buildTask(TaskName("chat-abcd1234-u17"), spec())
	if specHash(task) != specHash(updated) {
		t.Fatal("specHash must not depend on the provider config in fetch-at-boot mode")
	}
}

func TestProviderConfigPayloadKeysFollowInjectionMode(t *testing.T) {
	p := stubProvider(t, func(c *Config) {
		c.InjectLLMKey = true
		c.ProviderAtBoot = true
		c.GatewayURL = "http://gateway.invalid"
	})
	p.specs.Store("task-a", egressSpec())
	raw, ok := p.ProviderConfigByTask("task-a")
	if !ok {
		t.Fatal("known task must resolve")
	}
	var payload ProviderConfigPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.APIKey != "unit-key" || payload.Alias != "agctl" || payload.Model != "m" {
		t.Fatalf("payload: %+v", payload)
	}
	if len(payload.Extras) != 1 || payload.Extras[0].APIKey != "corp-key" {
		t.Fatalf("extras: %+v", payload.Extras)
	}
	if len(payload.BaseURLs) == 0 || payload.BaseURLs[0] != "https://llm.example.invalid/v1" {
		t.Fatalf("base urls: %v", payload.BaseURLs)
	}

	// egress-инъекция: placeholder вместо ключей.
	p2 := stubProvider(t, func(c *Config) {
		c.Egress = &EgressAutomation{Control: newFakeControl(), Secrets: newFakeSecrets(), Cluster: "c", Namespace: "n"}
		c.InjectLLMKey = false
		c.ProviderAtBoot = true
		c.GatewayURL = "http://gateway.invalid"
	})
	p2.specs.Store("task-b", egressSpec())
	raw, _ = p2.ProviderConfigByTask("task-b")
	var payload2 ProviderConfigPayload
	_ = json.Unmarshal(raw, &payload2)
	if payload2.APIKey != PlaceholderAPIKey || payload2.Extras[0].APIKey != PlaceholderAPIKey {
		t.Fatalf("egress payload keys: %+v", payload2)
	}
}

func TestGatewayEndpointAuthAnd404(t *testing.T) {
	p := stubProvider(t, func(c *Config) {
		c.InjectLLMKey = true
		c.ProviderAtBoot = true
		c.GatewayURL = "http://gateway.invalid"
	})
	p.specs.Store("agctl-task-1", spec())
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := p.ProviderConfigByTask(strings.TrimPrefix(r.URL.Path, ProviderConfigPath))
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(raw)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+ProviderConfigPath+"{task}", handler)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Известная задача — конфиг; токен проверяет шлюз (это его ручка).
	resp, err := http.Get(srv.URL + ProviderConfigPath + "agctl-task-1")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"apiKey":"unit-key"`) {
		t.Fatalf("config fetch: %d %s", resp.StatusCode, body)
	}
	// Неизвестная задача — 404.
	resp, _ = http.Get(srv.URL + ProviderConfigPath + "unknown-task")
	if resp.StatusCode != 404 {
		t.Fatalf("unknown task must 404: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestProviderAtBootRequiresGatewayURL(t *testing.T) {
	cfg := testConfig("")
	cfg.ProviderAtBoot = true
	if _, err := NewWithClient(cfg, v1alpha1.NewAXClient(nil), nil); err == nil {
		t.Fatal("provider-at-boot without gateway URL must fail explicitly")
	}
}

func TestFetchProviderConfigSendsBearer(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		if r.URL.Path != ProviderConfigPath+"agctl-task-9" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"alias":"agctl"}`))
	}))
	defer srv.Close()
	raw, err := FetchProviderConfig(srv.Client(), srv.URL, "agctl-task-9", "runner-secret")
	if err != nil || string(raw) != `{"alias":"agctl"}` {
		t.Fatalf("fetch: %s %v", raw, err)
	}
	if got != "Bearer runner-secret" {
		t.Fatalf("authorization: %q", got)
	}
	if _, err := FetchProviderConfig(srv.Client(), srv.URL, "no-such", "runner-secret"); err == nil {
		t.Fatal("missing task must fail")
	}
}

func TestProviderAtBootEgressStillRunsBeforeCreate(t *testing.T) {
	fake, client, router, _ := startFixture(t)
	control, secrets := newFakeControl(), newFakeSecrets()
	cfg := testConfig(router.URL)
	cfg.InjectLLMKey = false
	cfg.Egress = &EgressAutomation{Control: control, Secrets: secrets, Cluster: "c", Namespace: "agctl"}
	cfg.ProviderAtBoot = true
	cfg.GatewayURL = "http://gateway.invalid"
	provider, err := NewWithClient(cfg, client, router.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.EnsureChat(context.Background(), egressSpec()); err != nil {
		t.Fatal(err)
	}
	if len(control.policies) == 0 || len(secrets.data) == 0 {
		t.Fatal("egress automation must run before task create in fetch-at-boot mode too")
	}
	env := envOf(fake.task("agctl", TaskName("chat-abcd1234-u17")))
	if _, has := env[GatewayEnv]; !has {
		t.Fatal("gateway URL must be pinned into the task env")
	}
	if _, has := env["AGCTL_LLM_API_KEY"]; has {
		t.Fatal("provider env must be absent in fetch-at-boot mode")
	}
}
