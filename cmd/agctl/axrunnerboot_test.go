package main

// axrunnerboot_test.go — fetch-at-boot конфига провайдера: derivePublicURL
// и разбор payload шлюза раннером (providerAtBoot).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDerivePublicURL(t *testing.T) {
	cases := []struct {
		public, addr, want string
	}{
		{"https://agctl.corp.invalid/", "127.0.0.1:8929", "https://agctl.corp.invalid"},
		{"", "10.0.0.5:8929", "http://10.0.0.5:8929"},
		{"", "[::1]:8929", "http://[::1]:8929"},
		{"", "no-port", "no-port"},
	}
	for _, c := range cases {
		if got := derivePublicURL(c.public, c.addr); got != c.want {
			t.Fatalf("derivePublicURL(%q,%q) = %q; want %q", c.public, c.addr, got, c.want)
		}
	}
}

func TestProviderAtBootFromGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ax/provider-config/agctl-task-1" || r.Header.Get("Authorization") != "Bearer rt" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"alias":"agctl","baseUrls":["https://llm.example.invalid/v1"],"model":"m","models":["m","m2"],"apiKey":"unit-key","extras":[{"name":"corp","baseUrl":"https://litellm.corp.invalid/v1","model":"c","models":["c"],"apiKey":"corp-key"}]}`))
	}))
	defer srv.Close()
	p := providerAtBoot("agctl-task-1", srv.URL, "rt")
	if p == nil {
		t.Fatal("gateway config must resolve")
	}
	if p.Alias() != "agctl" || p.BaseURL != "https://llm.example.invalid/v1" || p.Model != "m" || p.APIKey != "unit-key" {
		t.Fatalf("provider: %+v", p.Provider)
	}
	if len(p.Extras) != 1 || p.Extras[0].Name != "corp" || p.Extras[0].APIKey != "corp-key" || p.Extras[0].APIKeyEnv == "" {
		t.Fatalf("extras: %+v", p.Extras)
	}
	// Шлюз недоступен — nil (раннер падает громко, а не молча без провайдера).
	if p := providerAtBoot("agctl-task-1", "http://127.0.0.1:1", "rt"); p != nil {
		t.Fatal("unreachable gateway must yield nil, not a degraded provider")
	}
	if p := providerAtBoot("agctl-task-1", "", "rt"); p != nil {
		t.Fatal("no gateway env must yield nil")
	}
}
