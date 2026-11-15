package server

import (
	"testing"

	"github.com/arijjia/agctl/internal/provision"
)

// TestSetDefaultLLMExtraKey — личный ключ доп. провайдера живо обновляется
// в провайдере сервера (и снимается при удалении секрета).
func TestSetDefaultLLMExtraKey(t *testing.T) {
	s := &Server{cfg: Config{DefaultLLM: provision.Provider{
		Extras: []provision.ExtraProvider{
			{Name: "corp-litellm", BaseURL: "https://llm.corp.example/v1", APIKeyEnv: "AGCTL_LLM_CORP_API_KEY"},
		},
	}}}
	s.setDefaultLLMExtraKey("AGCTL_LLM_CORP_API_KEY", "sk-personal-1")
	if got := s.cfg.DefaultLLM.Extras[0].APIKey; got != "sk-personal-1" {
		t.Fatalf("ключ не применился: %q", got)
	}
	s.setDefaultLLMExtraKey("AGCTL_LLM_CORP_API_KEY", "")
	if got := s.cfg.DefaultLLM.Extras[0].APIKey; got != "" {
		t.Fatalf("ключ не снялся: %q", got)
	}
}
