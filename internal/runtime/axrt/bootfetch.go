package axrt

// bootfetch.go — конфиг провайдера LLM для раннера песочницы при каждом
// boot (ProviderAtBoot): шлюз agctl отдаёт его по GET
// /ax/provider-config/<task> (Bearer = токен раннера), env задачи AX
// конфигурацию провайдера не содержит — смена профиля/ключа не меняет
// specHash задачи, раннер всегда стартует с актуальным конфигом.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	agctlrt "github.com/arijjia/agctl/internal/runtime"
)

// GatewayEnv — env задачи с базовым URL шлюза (фиксируется при создании
// задачи, не зависит от провайдера).
const GatewayEnv = "AGCTL_GATEWAY_URL"

// ProviderConfigPath — префикс пути эндпоинта на шлюзе (далее — имя
// задачи AX).
const ProviderConfigPath = "/ax/provider-config/"

// ProviderConfigPayload — JSON-конфиг провайдера для раннера. Семантика
// ключей та же, что у env задачи без fetch-at-boot: InjectLLMKey —
// реальный ключ; egress-инъекция — placeholder (реальный ключ подменит
// atenet-egress); иначе пусто (dsh спросит пользователя).
type ProviderConfigPayload struct {
	Alias    string               `json:"alias"`
	BaseURLs []string             `json:"baseUrls"`
	Model    string               `json:"model"`
	Models   []string             `json:"models"`
	APIKey   string               `json:"apiKey,omitempty"`
	Extras   []ExtraConfigPayload `json:"extras,omitempty"`
}

// ExtraConfigPayload — дополнительный провайдер с собственным ключом.
type ExtraConfigPayload struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"baseUrl"`
	Model   string   `json:"model"`
	Models  []string `json:"models"`
	APIKey  string   `json:"apiKey,omitempty"`
}

// ProviderConfigByTask — JSON конфига провайдера по имени задачи AX
// (спеку запоминает EnsureChat при ProviderAtBoot). ok=false — задача
// шлюзу неизвестна.
func (p *Provider) ProviderConfigByTask(task string) (json.RawMessage, bool) {
	raw, ok := p.specs.Load(task)
	if !ok {
		return nil, false
	}
	spec, ok := raw.(agctlrt.ChatSpec)
	if !ok {
		return nil, false
	}
	payload := ProviderConfigPayload{
		Alias:    spec.Provider.Alias(),
		BaseURLs: spec.Provider.AllBaseURLs(),
		Model:    spec.Provider.DefaultModel(),
		Models:   spec.Provider.AllModels(),
		APIKey:   p.payloadKey(spec.Provider.APIKey),
	}
	for _, extra := range spec.Provider.Extras {
		if len(extra.AllModels()) == 0 {
			continue
		}
		payload.Extras = append(payload.Extras, ExtraConfigPayload{
			Name: extra.Name, BaseURL: extra.BaseURL, Model: extra.DefaultModel(),
			Models: extra.AllModels(), APIKey: p.payloadKey(extra.APIKey),
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	return data, true
}

// payloadKey — ключ провайдера для раннера по режиму инъекции.
func (p *Provider) payloadKey(key string) string {
	if key == "" {
		return ""
	}
	switch {
	case p.cfg.InjectLLMKey:
		return key
	case p.cfg.Egress != nil:
		return PlaceholderAPIKey
	default:
		return "" // ручная egress-политика/без ключа: dsh спросит сам
	}
}

// FetchProviderConfig — GET {gateway}/ax/provider-config/{task} с Bearer
// токеном раннера (клиент раннера, cmd/agctl/axrunner.go).
func FetchProviderConfig(client *http.Client, gatewayURL, task, runnerToken string) ([]byte, error) {
	url := strings.TrimRight(gatewayURL, "/") + ProviderConfigPath + task
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+runnerToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider config: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
