package server

// axbootconfig_test.go — endpoint fetch-at-boot конфига провайдера для
// раннера AX (/ax/provider-config/<task>, Bearer = токен раннера).

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAxProviderConfigAuthAndPayload(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.AxRunnerToken = "runner-secret"
		c.AxProviderConfig = func(task string) (json.RawMessage, bool) {
			if task != "agctl-task-1" {
				return nil, false
			}
			return json.RawMessage(`{"alias":"agctl","model":"m"}`), true
		}
	})
	// Без токена — 404 (endpoint не раскрывает своё существование).
	resp, err := http.Get(env.api.URL + "/ax/provider-config/agctl-task-1")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("no token must 404: %d", resp.StatusCode)
	}
	// Чужой токен — 404.
	req, _ := http.NewRequest(http.MethodGet, env.api.URL+"/ax/provider-config/agctl-task-1", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err = env.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token must 404: %d", resp.StatusCode)
	}
	// Верный токен, известная задача — JSON конфиг.
	req.Header.Set("Authorization", "Bearer runner-secret")
	resp, err = env.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("known task: %d %s", resp.StatusCode, body)
	}
	var payload struct {
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Alias != "agctl" {
		t.Fatalf("payload: %s %v", body, err)
	}
	// Неизвестная задача — 404 при верном токене.
	req2, _ := http.NewRequest(http.MethodGet, env.api.URL+"/ax/provider-config/unknown", nil)
	req2.Header.Set("Authorization", "Bearer runner-secret")
	resp2, err := env.api.Client().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown task must 404: %d", resp2.StatusCode)
	}
}

func TestAxProviderConfigDisabledWithoutToken(t *testing.T) {
	env := newTestEnvWith(t, 3, func(c *Config) {
		c.AxProviderConfig = func(string) (json.RawMessage, bool) {
			return json.RawMessage(`{}`), true
		}
	})
	req, _ := http.NewRequest(http.MethodGet, env.api.URL+"/ax/provider-config/agctl-task-1", nil)
	req.Header.Set("Authorization", "Bearer anything")
	resp, err := env.api.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("endpoint must be off without token: %d", resp.StatusCode)
	}
	if !strings.Contains(http.StatusText(resp.StatusCode), "Not Found") {
		t.Fatalf("unexpected status text: %s", http.StatusText(resp.StatusCode))
	}
}
