package provision

// llm_sync.go — обратная синхронизация настроек моделей: харнесс → хост.
//
// Пользователь может править провайдеров и ключи прямо в настройках dsh
// (Settings → Models). dsh пишет их в dsh-home: состав провайдеров — в
// profiles/web/cordis.patch.yml (строка llm-pi-ai, config.providers), значения
// ключей — в .credentials.yaml (refs: ИМЯ → ключ). agctl при каждом старте
// рабочего места перезаписывает оба файла из настроек хоста, поэтому без
// обратного чтения правки из dsh терялись.
//
// Схема трёхсторонняя: рядом с тем, что agctl записал, хранится снимок
// (.agctl/llm-baseline.json: состав провайдеров и хеши ключей, без значений).
// Отличие текущего состояния dsh-home от снимка — это правка, сделанная в
// харнессе; её и переносят на хост. Правка, сделанная на хосте, от снимка не
// отличается и обратно не едет — стороны не затирают друг друга.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

const (
	llmBaselineRel = ".agctl/llm-baseline.json"
	llmPluginName  = "@deepseek-ai/dsh-llm-pi-ai"
)

// HarnessProvider — провайдер, как он записан в dsh-home.
type HarnessProvider struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"baseURL"`
	Models  []string `json:"models"`
	KeyEnv  string   `json:"keyEnv"`
	// KeyHash — sha256 значения ключа ("" — ключа нет). Значение в снимок
	// не пишется.
	KeyHash string `json:"keyHash"`
	// Key — значение ключа из .credentials.yaml; только в памяти.
	Key string `json:"-"`
}

// HarnessChange — правка, сделанная в харнессе относительно снимка agctl.
type HarnessChange struct {
	// Kind: added — провайдер добавлен в харнессе; changed — изменены адрес,
	// модели или ключ; removed — провайдер удалён в харнессе.
	Kind          string
	Provider      HarnessProvider
	ConfigChanged bool // адрес или список моделей
	KeyChanged    bool // значение ключа (Provider.Key — новое; "" — ключ убран)
}

// ReadHarnessLLM читает провайдеров и ключи из dsh-home.
func ReadHarnessLLM(home string) (map[string]HarnessProvider, error) {
	out := map[string]HarnessProvider{}
	raw, err := os.ReadFile(filepath.Join(home, "profiles", "web", "cordis.patch.yml")) // #nosec G304
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("харнесс: патч профиля: %w", err)
	}
	var entries []map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &entries); err != nil {
			return nil, fmt.Errorf("харнесс: патч профиля: %w", err)
		}
	}
	refs := map[string]string{}
	if craw, err := os.ReadFile(filepath.Join(home, ".credentials.yaml")); err == nil { // #nosec G304
		var creds struct {
			Refs map[string]string `yaml:"refs"`
		}
		if yaml.Unmarshal(craw, &creds) == nil && creds.Refs != nil {
			refs = creds.Refs
		}
	}
	for _, entry := range entries {
		if name, _ := entry["name"].(string); name != llmPluginName {
			continue
		}
		cfg, _ := entry["config"].(map[string]any)
		providers, _ := cfg["providers"].(map[string]any)
		for alias, v := range providers {
			row, _ := v.(map[string]any)
			if row == nil {
				continue
			}
			p := HarnessProvider{Name: alias}
			p.BaseURL, _ = row["baseURL"].(string)
			p.KeyEnv, _ = row["apiKeyEnv"].(string)
			if models, ok := row["models"].([]any); ok {
				for _, m := range models {
					switch mv := m.(type) {
					case map[string]any:
						if id, _ := mv["id"].(string); id != "" {
							p.Models = append(p.Models, id)
						}
					case string:
						p.Models = append(p.Models, mv)
					}
				}
			}
			if key := refs[p.KeyEnv]; key != "" {
				p.Key = key
				sum := sha256.Sum256([]byte(key))
				p.KeyHash = hex.EncodeToString(sum[:])
			}
			out[alias] = p
		}
	}
	return out, nil
}

// WriteLLMBaseline запоминает текущее состояние dsh-home как «записанное
// agctl» (или уже перенесённое на хост).
func WriteLLMBaseline(home string) error {
	cur, err := ReadHarnessLLM(home)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(cur, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(home, filepath.FromSlash(llmBaselineRel))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, raw, 0o600)
}

// DiffHarnessLLM — правки харнесса относительно снимка. Снимка ещё нет
// (dsh-home не стартовал под этой версией agctl) — правок нет.
func DiffHarnessLLM(home string) ([]HarnessChange, error) {
	raw, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(llmBaselineRel))) // #nosec G304
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	base := map[string]HarnessProvider{}
	if err := json.Unmarshal(raw, &base); err != nil {
		return nil, fmt.Errorf("харнесс: снимок настроек моделей: %w", err)
	}
	cur, err := ReadHarnessLLM(home)
	if err != nil {
		return nil, err
	}
	var out []HarnessChange
	for name, p := range cur {
		b, known := base[name]
		if !known {
			out = append(out, HarnessChange{Kind: "added", Provider: p, ConfigChanged: true, KeyChanged: p.Key != ""})
			continue
		}
		cfgChanged := b.BaseURL != p.BaseURL || !sameStrings(b.Models, p.Models)
		keyChanged := b.KeyHash != p.KeyHash
		if cfgChanged || keyChanged {
			out = append(out, HarnessChange{Kind: "changed", Provider: p, ConfigChanged: cfgChanged, KeyChanged: keyChanged})
		}
	}
	for name, b := range base {
		if _, still := cur[name]; !still {
			out = append(out, HarnessChange{Kind: "removed", Provider: b})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider.Name < out[j].Provider.Name })
	return out, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
