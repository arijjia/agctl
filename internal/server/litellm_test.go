package server

// litellm_test.go — enforcement квот виртуальными ключами LiteLLM
// (карточка litellm-quota-enforcement): маппинг квота→ключ, ротация при
// смене, отзыв при снятии квоты, персональный секрет не трогаем.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/arijjia/agctl/internal/provision"
	"github.com/arijjia/agctl/internal/secrets"
)

// fakeLiteLLM — стаб admin-API: помнит generate-запросы и ревоки.
type fakeLiteLLM struct {
	mu        sync.Mutex
	generated []map[string]any
	revoked   []string
	n         int
	srv       *httptest.Server
}

func newFakeLiteLLM(t *testing.T) *fakeLiteLLM {
	t.Helper()
	f := &fakeLiteLLM{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /key/generate", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer admin-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.n++
		f.generated = append(f.generated, req)
		key := fmt.Sprintf("sk-virt-%d", f.n)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"key": key, "token_id": "tok-" + key})
	})
	mux.HandleFunc("POST /key/delete", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Keys []string `json:"keys"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.revoked = append(f.revoked, req.Keys...)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLiteLLM) url() string { return f.srv.URL }

func TestQuotaAwareProviderMintsVirtualKey(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) {
		c.LiteLLMQuotas = true
		c.LiteLLMAdminURL = "will-be-set"
		c.LiteLLMAdminKey = "admin-secret"
	})
	fake := newFakeLiteLLM(t)
	e.srv.cfg.LiteLLMAdminURL = fake.url()
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "http://litellm.invalid/v1", Model: "m", APIKey: "sk-platform"}

	ids := prewarmSeedActivity(t, e, []string{"alice"}) // role: user
	// Квоты нет — платформенный ключ как есть, ключей не минтим.
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-platform" {
		t.Fatalf("без квоты ключ = %q, want платформенный", got)
	}
	// Персональная квота: дневной бюджет → max_budget=Daily, duration=1d.
	if err := e.db.QuotaUpsert("user", "alice", 5000, 0, ""); err != nil {
		t.Fatal(err)
	}
	p := e.srv.quotaAwareProvider(ids[0])
	if p.APIKey != "sk-virt-1" {
		t.Fatalf("ключ = %q, want sk-virt-1", p.APIKey)
	}
	if p.BaseURL != e.srv.cfg.DefaultLLM.BaseURL {
		t.Fatal("baseURL изменился")
	}
	fake.mu.Lock()
	gen := fake.generated[len(fake.generated)-1]
	fake.mu.Unlock()
	if gen["max_budget"] != float64(5000) || gen["budget_duration"] != "1d" {
		t.Fatalf("generate-запрос: %+v", gen)
	}
	// Повторный вызов — ключ из кэша, без нового минта.
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-virt-1" {
		t.Fatalf("кэш ключа не работает: %q", got)
	}
}

func TestQuotaChangeRotatesKey(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) {
		c.LiteLLMQuotas = true
		c.LiteLLMAdminKey = "admin-secret"
	})
	fake := newFakeLiteLLM(t)
	e.srv.cfg.LiteLLMAdminURL = fake.url()
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "http://litellm.invalid/v1", Model: "m", APIKey: "sk-platform"}

	ids := prewarmSeedActivity(t, e, []string{"bob"})
	if err := e.db.QuotaUpsert("user", "bob", 1000, 0, ""); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-virt-1" {
		t.Fatalf("первый ключ: %q", got)
	}
	// Смена квоты через админ-API: старый ключ отозван, наминчен новый.
	if code, body := e.do(e.admin, "PUT", "/api/admin/quotas", map[string]any{
		"scope": "user", "subject": "bob", "dailyTokens": 2000,
	}); code != http.StatusOK {
		t.Fatalf("quota put: %d %s", code, body)
	}
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-virt-2" {
		t.Fatalf("после смены квоты ключ: %q, want sk-virt-2 (ротация)", got)
	}
	fake.mu.Lock()
	revoked := append([]string(nil), fake.revoked...)
	fake.mu.Unlock()
	if len(revoked) == 0 || revoked[0] != "sk-virt-1" {
		t.Fatalf("старый ключ не отозван: %v", revoked)
	}
	// Снятие квоты: ключ отозван, платформенный ключ без минта.
	if code, _ := e.do(e.admin, "DELETE", "/api/admin/quotas/1", nil); code != http.StatusOK {
		t.Fatal("quota delete не прошёл")
	}
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-platform" {
		t.Fatalf("после снятия квоты ключ: %q, want платформенный", got)
	}
	fake.mu.Lock()
	revoked = append([]string(nil), fake.revoked...)
	fake.mu.Unlock()
	if len(revoked) != 2 || revoked[1] != "sk-virt-2" {
		t.Fatalf("ключ квоты не отозван при снятии: %v", revoked)
	}
}

func TestQuotaProviderIgnoresPersonalSecret(t *testing.T) {
	e := newTestEnvWith(t, 3, func(c *Config) {
		c.LiteLLMQuotas = true
		c.LiteLLMAdminKey = "admin-secret"
	})
	fake := newFakeLiteLLM(t)
	e.srv.cfg.LiteLLMAdminURL = fake.url()
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "http://litellm.invalid/v1", Model: "m", APIKey: "sk-platform"}

	ids := prewarmSeedActivity(t, e, []string{"carol"})
	if err := e.db.QuotaUpsert("user", "carol", 1000, 0, ""); err != nil {
		t.Fatal(err)
	}
	// Личный секрет пользователя в KV — свой провайдер, квоты не применяем.
	keyPath, err := secrets.EnsureMasterKey(e.home)
	if err != nil {
		t.Fatal(err)
	}
	sec, err := secrets.New(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := sec.Encrypt("sk-personal")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.SecretUpsert(ids[0], "http://own.example/v1", "own-model", []string{"own-model"}, ct); err != nil {
		t.Fatal(err)
	}
	p := e.srv.quotaAwareProvider(ids[0])
	if p.BaseURL != "http://own.example/v1" || p.APIKey != "sk-personal" {
		t.Fatalf("личный провайдер заменён виртуальным ключом: %+v", p)
	}
	fake.mu.Lock()
	n := len(fake.generated)
	fake.mu.Unlock()
	if n != 0 {
		t.Fatalf("для личного провайдера наминчен ключ: %d", n)
	}
}

func TestQuotaDisabledByDefault(t *testing.T) {
	e := newTestEnv(t, 3) // LiteLLMQuotas=false
	fake := newFakeLiteLLM(t)
	e.srv.cfg.LiteLLMAdminURL = fake.url()
	e.srv.cfg.LiteLLMAdminKey = "admin-secret"
	e.srv.cfg.DefaultLLM = provision.Provider{BaseURL: "http://litellm.invalid/v1", Model: "m", APIKey: "sk-platform"}
	ids := prewarmSeedActivity(t, e, []string{"dave"})
	if err := e.db.QuotaUpsert("user", "dave", 1000, 0, ""); err != nil {
		t.Fatal(err)
	}
	if got := e.srv.quotaAwareProvider(ids[0]).APIKey; got != "sk-platform" {
		t.Fatalf("при выключенном флаге ключ заменён: %q", got)
	}
	fake.mu.Lock()
	n := len(fake.generated)
	fake.mu.Unlock()
	if n != 0 {
		t.Fatalf("заминчено ключей при выключенном enforcement: %d", n)
	}
}
