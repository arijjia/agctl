// jirapoll.go — поллер метки agent-run: раз в минуту спрашивает Jira по JQL
// и ставит новые задачи в очередь jira_runs целевого воркспейса.
//
// Это СКЕЛЕТ по итогам спайка jira-trigger-spike: доставка до agctl работает,
// инжект промпта в dsh-сессию — следующий шаг (нужна проба HTTP endpoints
// dsh 0.1.7-rc.2 на живом контейнере; запасной вектор — Runtime.Exec).
//
// Настройка — только env оператора (в БД не храним):
//
//	AGCTL_JIRA_URL      — например https://jira.utmn.ru
//	AGCTL_JIRA_TOKEN    — PAT сервис-аккаунта агента (Bearer)
//	AGCTL_JIRA_JQL      — по умолчанию: labels = agent-run
//	AGCTL_JIRA_WORKSPACE— id воркспейса-исполнителя (w-…); без него поллер спит
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const jiraPollEvery = time.Minute

// StartJiraPoller — фоновый цикл поллера (вызывается из StartBackground).
func (s *Server) StartJiraPoller(ctx context.Context) {
	go func() {
		t := time.NewTicker(jiraPollEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.pollJira(ctx)
			}
		}
	}()
}

type jiraSearchHit struct {
	Key string `json:"key"`
}

// pollJira — один проход: JQL → новые ключи → очередь jira_runs.
func (s *Server) pollJira(ctx context.Context) {
	base := strings.TrimRight(os.Getenv("AGCTL_JIRA_URL"), "/")
	token := os.Getenv("AGCTL_JIRA_TOKEN")
	wsID := os.Getenv("AGCTL_JIRA_WORKSPACE")
	if base == "" || token == "" || wsID == "" {
		return // поллер не настроен — тихо спим
	}
	if w, err := s.db.WorkspaceByID(wsID); err != nil || w.ID == "" || w.Status != "running" {
		return // целевого running-воркспейса нет — ждать
	}
	jql := os.Getenv("AGCTL_JIRA_JQL")
	if jql == "" {
		jql = "labels = agent-run"
	}
	q := url.Values{"jql": {jql}, "maxResults": {"10"}, "fields": {"key"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/rest/api/2/search?"+q.Encode(), nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[jira-poll] недоступна: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[jira-poll] статус %d — проверь токен/JQL", resp.StatusCode)
		return
	}
	var data struct {
		Issues []jiraSearchHit `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Printf("[jira-poll] ответ не разбирается: %v", err)
		return
	}
	for _, hit := range data.Issues {
		if hit.Key == "" {
			continue
		}
		// новые ключи даёт только INSERT OR IGNORE — повторных запусков нет
		if err := s.db.JiraRunInsert(hit.Key, wsID); err != nil {
			log.Printf("[jira-poll] %s: очередь: %v", hit.Key, err)
			continue
		}
		_ = s.db.Audit("jira-poller", "jira.run.queued", wsID, fmt.Sprintf("issue=%s", hit.Key))
		log.Printf("[jira-poll] %s → очередь воркспейса %s (инжект: следующий шаг)", hit.Key, wsID)
		// TODO(jira-trigger): инжект промпта в dsh-сессию воркспейса
		// (HTTP endpoints dsh 0.1.7-rc.2, bearer = wsToken) → JiraRunUpdate(key, "injected", sessionID).
	}
}
