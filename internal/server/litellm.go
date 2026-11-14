package server

// litellm.go — enforcement квот токенов через виртуальные ключи LiteLLM
// (карточка litellm-quota-enforcement). Квоты из админки (SQLite) видны,
// но трафик ходит с одним платформенным ключом. При включении
// (--litellm-quotas + env AGCTL_LITELLM_ADMIN_URL/AGCTL_LITELLM_ADMIN_KEY)
// agctl минтит персональный виртуальный ключ LiteLLM (POST /key/generate)
// с max_budget/budget_duration из действующей квоты пользователя и
// подставляет его вместо платформенного ключа при старте рантайма.
// Admin-ключ LiteLLM живёт только в env процесса serve: не в гите, не в
// образе, не в логах.
//
// Маппинг квота→бюджет: дневной бюджет приоритетен (max_budget=DailyTokens,
// budget_duration="1d" — обнуляется каждый день), иначе месячный
// (MonthlyTokens, "30d"). Единицы max_budget интерпретирует LiteLLM
// (валюта прокси); числа квот передаются как есть.
//
// Отзыв: при смене/удалении квоты (PUT/DELETE /api/admin/quotas) ключ
// отзывается (POST /key/delete) и кэш сбрасывается — следующий старт
// рантайма наминтит новый. Персональный LLM-секрет пользователя квотами
// не трогается: трафик идёт с его собственным ключом мимо платформы.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/arijjia/agctl/internal/provision"
)

// llmKeyState — заминченный виртуальный ключ пользователя (для ротации
// при смене квоты: сигнатура daily/monthly).
type llmKeyState struct {
	key     string
	keyID   string
	daily   int64
	monthly int64
}

type litellmClient struct {
	baseURL  string
	adminKey string
	http     *http.Client
}

func (c *litellmClient) do(path string, req any, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, strings.TrimRight(c.baseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.adminKey)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("litellm %s: статус %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// generate минтит виртуальный ключ с бюджетом.
func (c *litellmClient) generate(maxBudget int64, duration string) (key, keyID string, err error) {
	var out struct {
		Key     string `json:"key"`
		TokenID string `json:"token_id"`
		Info    struct {
			Key string `json:"key"`
		} `json:"info"`
	}
	if err := c.do("/key/generate", map[string]any{
		"max_budget":      maxBudget,
		"budget_duration": duration,
	}, &out); err != nil {
		return "", "", err
	}
	key = out.Key
	keyID = out.TokenID
	if keyID == "" {
		keyID = out.Info.Key
	}
	if key == "" {
		return "", "", fmt.Errorf("litellm /key/generate: пустой ключ в ответе")
	}
	return key, keyID, nil
}

// revoke удаляет виртуальные ключи.
func (c *litellmClient) revoke(keys ...string) error {
	return c.do("/key/delete", map[string]any{"keys": keys}, new(any))
}

// llmKeys — кэш виртуальных ключей по userID.
func (s *Server) llmClient() *litellmClient {
	if !s.cfg.LiteLLMQuotas || s.cfg.LiteLLMAdminURL == "" || s.cfg.LiteLLMAdminKey == "" {
		return nil
	}
	return &litellmClient{baseURL: s.cfg.LiteLLMAdminURL, adminKey: s.cfg.LiteLLMAdminKey, http: &http.Client{Timeout: 15 * time.Second}}
}

// quotaBudget — бюджет действующей квоты: (max, duration, есть).
func (s *Server) quotaBudget(userID int64) (int64, string, bool) {
	u, err := s.db.UserByID(userID)
	if err != nil {
		return 0, "", false
	}
	q, err := s.db.QuotaEffectiveForUser(u.Role, u.Login)
	if err != nil || q.ID == 0 {
		return 0, "", false
	}
	switch {
	case q.DailyTokens > 0:
		return q.DailyTokens, "1d", true
	case q.MonthlyTokens > 0:
		return q.MonthlyTokens, "30d", true
	}
	return 0, "", false
}

// quotaAwareProvider — провайдер чата с enforcement-ключом: персональный
// секрет пользователя не трогаем; платформенный ключ заменяем виртуальным
// по квоте. Ошибка LiteLLM не блокирует чат — платформенный ключ как есть
// (деградация видна в логе).
func (s *Server) quotaAwareProvider(userID int64) provision.Provider {
	base := s.userProvider(userID)
	if base.APIKey == "" || base.BaseURL == "" {
		return base
	}
	if def := s.defaultLLM(); base.APIKey != def.APIKey || base.BaseURL != def.BaseURL {
		return base // персональный провайдер/секрет пользователя
	}
	cli := s.llmClient()
	if cli == nil {
		return base
	}
	max, duration, has := s.quotaBudget(userID)
	if !has {
		// квоту сняли: отозвать осиротевший ключ, платформенный ключ как есть
		s.revokeUserKey(userID, "квота снята")
		return base
	}
	q := s.effectiveQuota(userID)
	s.llmMu.Lock()
	st := s.llmKeys[userID]
	s.llmMu.Unlock()
	if st != nil && st.daily == q.daily && st.monthly == q.monthly && st.key != "" {
		out := base
		out.APIKey = st.key
		return out
	}
	key, keyID, err := cli.generate(max, duration)
	if err != nil {
		log.Printf("[litellm] пользователь %d: виртуальный ключ не наминтился (%v) — платформенный ключ без квоты", userID, err)
		return base
	}
	if st != nil && st.key != "" {
		go func(old string) { _ = cli.revoke(old) }(st.key)
	}
	s.llmMu.Lock()
	if s.llmKeys == nil {
		s.llmKeys = map[int64]*llmKeyState{}
	}
	s.llmKeys[userID] = &llmKeyState{key: key, keyID: keyID, daily: q.daily, monthly: q.monthly}
	s.llmMu.Unlock()
	log.Printf("[litellm] пользователь %d: виртуальный ключ %s наминчен (бюджет %d, %s)", userID, shortKey(key), max, duration)
	out := base
	out.APIKey = key
	return out
}

// quotaSig — сигнатура действующей квоты (для ротации ключа при смене).
type quotaSig struct{ daily, monthly int64 }

func (s *Server) effectiveQuota(userID int64) quotaSig {
	u, err := s.db.UserByID(userID)
	if err != nil {
		return quotaSig{}
	}
	q, err := s.db.QuotaEffectiveForUser(u.Role, u.Login)
	if err != nil {
		return quotaSig{}
	}
	return quotaSig{daily: q.DailyTokens, monthly: q.MonthlyTokens}
}

// revokeUserKey — отзыв ключа пользователя (снятие квоты/удаление учётки).
func (s *Server) revokeUserKey(userID int64, why string) {
	s.llmMu.Lock()
	st := s.llmKeys[userID]
	delete(s.llmKeys, userID)
	s.llmMu.Unlock()
	if st == nil || st.key == "" {
		return
	}
	if cli := s.llmClient(); cli != nil {
		if err := cli.revoke(st.key); err != nil {
			log.Printf("[litellm] пользователь %d: отзыв ключа (%s) не удался: %v", userID, why, err)
			return
		}
	}
	log.Printf("[litellm] пользователь %d: виртуальный ключ отозван (%s)", userID, why)
}

// invalidateQuotaKeys — сброс кэшей при смене квот в админке: user-scope —
// точечно по логину, role-scope — по всем (квота роли бьёт пачку людей).
func (s *Server) invalidateQuotaKeys(scope, subject string) {
	if scope == "user" {
		if u, err := s.db.UserByLogin(subject); err == nil {
			s.revokeUserKey(u.ID, "квота изменена")
		}
		return
	}
	s.llmMu.Lock()
	old := s.llmKeys
	s.llmKeys = map[int64]*llmKeyState{}
	s.llmMu.Unlock()
	if cli := s.llmClient(); cli != nil {
		for _, st := range old {
			if st.key != "" {
				_ = cli.revoke(st.key)
			}
		}
	}
	if len(old) > 0 {
		log.Printf("[litellm] квота роли %q изменена: отозвано ключей: %d", subject, len(old))
	}
}

// shortKey — ключ для лога без секрета.
func shortKey(key string) string {
	if len(key) <= 8 {
		return "sk-…"
	}
	return key[:5] + "…"
}
