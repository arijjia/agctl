package main

// dev_llmcheck.go — `agctl dev llm-check`: модели профиля и личные провайдеры
// дома глазами агента — отвечает ли модель и умеет ли она вызывать
// инструменты (tool calling с tool_choice=auto). Без вызова инструментов
// агент dsh на модели работать не сможет, поэтому новый шлюз моделей
// (корпоративный LiteLLM, vLLM) проверяется здесь до показа рабочего места.
// Запросы идут с этой машины, не из контейнера; ключи — из хранилища дома
// (перед проверкой пустые подхватываются из окружения), наружу не печатаются.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/cli"
	"github.com/arijjia/agctl/internal/localmode"
	"github.com/arijjia/agctl/internal/profile"
)

// llmCheckProvider — провайдер моделей для проверки (без ключа).
type llmCheckProvider struct {
	Name    string
	BaseURL string
	KeyEnv  string
	Models  []string
}

// devLLMProviders — провайдеры, которые увидит агент: основной и
// дополнительные из профиля, личные из config.yaml дома.
func devLLMProviders(cfg *localmode.Config, prof *profile.Data) []llmCheckProvider {
	var out []llmCheckProvider
	if prof != nil {
		v := prof.View()
		if base := strings.TrimSpace(v.LLMBaseURL); base != "" {
			out = append(out, llmCheckProvider{
				Name: cli.FirstNonEmpty(v.LLMProvider, "agctl"), BaseURL: base, KeyEnv: "AGCTL_LLM_API_KEY",
				Models: llmModelList(v.LLMModels, v.LLMModel),
			})
		}
		for _, e := range v.LLMExtraProviders {
			if strings.TrimSpace(e.BaseURL) == "" {
				continue
			}
			out = append(out, llmCheckProvider{
				Name: e.Name, BaseURL: strings.TrimSpace(e.BaseURL), KeyEnv: strings.TrimSpace(e.APIKeyEnv),
				Models: llmModelList(e.Models, e.Model),
			})
		}
	}
	for _, p := range cfg.Providers {
		out = append(out, llmCheckProvider{
			Name: p.Name, BaseURL: p.BaseURL, KeyEnv: localmode.ProviderKeyEnv(p.Name),
			Models: llmModelList(strings.Join(p.Models, ","), p.DefaultModel),
		})
	}
	return out
}

// llmModelList — модели из списка через запятую; пусто — модель по умолчанию.
func llmModelList(csv, fallback string) []string {
	var out []string
	for _, m := range strings.Split(csv, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	if len(out) == 0 && strings.TrimSpace(fallback) != "" {
		out = []string{strings.TrimSpace(fallback)}
	}
	return out
}

func devLLMCheck(home string, args []string) error {
	fs := flag.NewFlagSet("dev llm-check", flag.ExitOnError)
	only := fs.String("model", "", "проверить одну модель")
	onlyProvider := fs.String("provider", "", "проверить одного провайдера (имя из профиля или «Доступа»)")
	insecure := fs.Bool("insecure", false, "не проверять TLS-сертификат (только диагностика: отличить сертификат шлюза от модели)")
	timeout := fs.Duration("timeout", 90*time.Second, "предел на один запрос")
	_ = fs.Parse(args)
	cfg, err := localmode.Load(home)
	if err != nil {
		return err
	}
	devImportHostEnv(home, cfg)
	if cfg, err = localmode.Load(home); err != nil {
		return err
	}
	prof, err := devResolveProfile(cfg)
	if err != nil {
		return err
	}
	store := localmode.OpenStore(home)
	client := &http.Client{Timeout: *timeout}
	if *insecure {
		// #nosec G402 -- явный флаг диагностики, по умолчанию TLS проверяется
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		fmt.Println("⚠ --insecure: сертификаты не проверяются — только чтобы отличить TLS от модели")
	}
	providers := devLLMProviders(cfg, prof)
	if len(providers) == 0 {
		return errors.New("в профиле и доме нет провайдеров моделей с адресом")
	}
	checked, bad := 0, 0
	for _, p := range providers {
		if *onlyProvider != "" && p.Name != *onlyProvider {
			continue
		}
		key := ""
		if p.KeyEnv != "" {
			key, _ = store.Get(p.KeyEnv)
		}
		keyState := "задан"
		if key == "" {
			keyState = "НЕ задан («Доступ» или переменная окружения)"
		}
		fmt.Printf("\n%s · %s · ключ %s: %s\n", p.Name, p.BaseURL, cli.FirstNonEmpty(p.KeyEnv, "—"), keyState)
		if ids, err := llmListModels(client, p.BaseURL, key); err != nil {
			fmt.Printf("  GET /models: ✗ %s\n", llmHideKey(err.Error(), key))
		} else {
			known := map[string]bool{}
			for _, id := range ids {
				known[id] = true
			}
			var absent []string
			for _, m := range p.Models {
				if !known[m] {
					absent = append(absent, m)
				}
			}
			fmt.Printf("  GET /models: %d — %s\n", len(ids), strings.Join(llmFirst(ids, 10), ", "))
			if len(absent) > 0 && len(ids) > 0 {
				fmt.Printf("  ⚠ шлюз не перечисляет: %s\n", strings.Join(absent, ", "))
			}
		}
		for _, m := range p.Models {
			if *only != "" && m != *only {
				continue
			}
			checked++
			res := llmProbe(client, p.BaseURL, key, m)
			if !res.ToolsOK {
				bad++
			}
			fmt.Printf("  %-44s %s\n", m, llmHideKey(res.String(), key))
		}
	}
	if checked == 0 {
		return errors.New("ни одной модели под условия --model / --provider")
	}
	if bad > 0 {
		return fmt.Errorf("моделей без ответа или без вызова инструментов: %d из %d — агент dsh на них работать не сможет (чат без инструментов — возможен)", bad, checked)
	}
	fmt.Printf("\n✓ все модели (%d) отвечают и вызывают инструменты\n", checked)
	return nil
}

// llmProbeResult — итог проверки одной модели.
type llmProbeResult struct {
	ToolsOK  bool          // вызвала инструмент
	TextOnly bool          // ответила текстом, инструмент не вызвала
	ToolsErr string        // ошибка запроса с инструментами
	ChatOK   bool          // отвечает без инструментов
	ChatErr  string        // ошибка простого запроса
	Took     time.Duration // время запроса с инструментами
}

func (r llmProbeResult) String() string {
	switch {
	case r.ToolsOK:
		return fmt.Sprintf("✓ инструменты (%.1f с)", r.Took.Seconds())
	case r.TextOnly:
		return fmt.Sprintf("~ ответила текстом, инструмент не вызвала (%.1f с) — агент на ней работать будет плохо", r.Took.Seconds())
	case r.ChatOK:
		return "✗ инструменты: " + r.ToolsErr + " · без инструментов отвечает"
	default:
		return "✗ не отвечает: " + cli.FirstNonEmpty(r.ChatErr, r.ToolsErr)
	}
}

// llmProbe — запрос с одним инструментом и tool_choice=auto (как у агента),
// при ошибке — простой запрос без инструментов.
func llmProbe(c *http.Client, base, key, model string) llmProbeResult {
	var r llmProbeResult
	start := time.Now()
	out, err := llmPost(c, base, key, map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "Который час? Ответь только вызовом инструмента get_time."},
		},
		"tools": []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_time",
				"description": "Текущее время",
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
		"tool_choice": "auto",
		"max_tokens":  512,
	})
	r.Took = time.Since(start)
	if err == nil {
		msg := llmMessage(out)
		if calls, _ := msg["tool_calls"].([]any); len(calls) > 0 {
			r.ToolsOK, r.ChatOK = true, true
			return r
		}
		r.TextOnly, r.ChatOK = true, true
		return r
	}
	r.ToolsErr = err.Error()
	if _, err := llmPost(c, base, key, map[string]any{
		"model":      model,
		"messages":   []map[string]string{{"role": "user", "content": "Ответь одним словом: ок"}},
		"max_tokens": 256,
	}); err != nil {
		r.ChatErr = err.Error()
	} else {
		r.ChatOK = true
	}
	return r
}

// llmMessage — choices[0].message ответа chat/completions.
func llmMessage(out map[string]any) map[string]any {
	choices, _ := out["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	return msg
}

func llmPost(c *http.Client, base, key string, body any) (map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	data, status, err := llmDo(c, req)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", status, llmOneLine(data, 220))
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("ответ не JSON: %s", llmOneLine(data, 120))
	}
	return out, nil
}

// llmListModels — id моделей из GET /models (OpenAI-совместимый список).
func llmListModels(c *http.Client, base, key string) ([]string, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	data, status, err := llmDo(c, req)
	if err != nil {
		return nil, err
	}
	if status/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", status, llmOneLine(data, 160))
	}
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("ответ не JSON: %s", llmOneLine(data, 120))
	}
	ids := make([]string, 0, len(doc.Data))
	for _, d := range doc.Data {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

func llmDo(c *http.Client, req *http.Request) ([]byte, int, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return data, resp.StatusCode, err
}

// llmOneLine — начало тела ответа одной строкой.
func llmOneLine(b []byte, n int) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}

func llmFirst(ids []string, n int) []string {
	if len(ids) <= n {
		return ids
	}
	return append(append([]string(nil), ids[:n]...), fmt.Sprintf("… ещё %d", len(ids)-n))
}

// llmHideKey — ключ не печатается, даже если шлюз вернул его в ошибке.
func llmHideKey(s, key string) string {
	if len(key) < 6 {
		return s
	}
	return strings.ReplaceAll(s, key, "<ключ>")
}
