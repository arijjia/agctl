package server

// agent_creds.go — доступы рабочего места (GitLab/Jira/мост) для команд
// агента. Инструмент bash в dsh запускает команды с вычищенным окружением:
// переменные с KEY/TOKEN/SECRET/PASSWORD в имени до агента не доходят
// (dsh-subprocess-local, эвристика по имени). Поэтому доступы кладём файлами
// в dsh-home (0600; не /workspace — там $HOME контейнера и, в локальном
// режиме, настоящие репозитории разработчика), а инструменты находят их по
// переменным без «секретных» слов в имени: GIT_CONFIG_GLOBAL, GLAB_CONFIG_DIR,
// AGCTL_CREDENTIALS_ENV. Мост к хосту читает свой токен из bridge.json.

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arijjia/agctl/internal/localbridge"
)

// containerDshHome — DSH_HOME внутри контейнера (маунт dockerrt).
const containerDshHome = "/home/dsh"

// agentCredsDir — каталог доступов в dsh-home (имя без точки-префикса в
// контейнере: /home/dsh/.agctl).
const agentCredsDir = ".agctl"

// agentCredsEnv — переменные контейнера, указывающие инструментам на файлы
// доступов (их имена переживают чистку окружения dsh).
func agentCredsEnv(hasGit bool) []string {
	base := containerDshHome + "/" + agentCredsDir
	env := []string{"AGCTL_CREDENTIALS_ENV=" + base + "/credentials.env"}
	if hasGit {
		env = append(env, "GIT_CONFIG_GLOBAL="+base+"/gitconfig", "GLAB_CONFIG_DIR="+base+"/glab")
	}
	return env
}

// writeAgentCredentials пишет файлы доступов в dsh-home пользователя по env
// рабочего места (KEY=VALUE). Возвращает доп. переменные контейнера.
// Пустой env — файлов нет, старые удаляются (доступ отозван).
func (s *Server) writeAgentCredentials(userID int64, env []string) []string {
	dir := filepath.Join(s.projectDshHome(userID), agentCredsDir)
	vals := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && v != "" {
			vals[k] = v
		}
	}
	if len(vals) == 0 {
		_ = os.RemoveAll(dir)
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("[creds] u%d: %v", userID, err)
		return nil
	}
	_ = os.Chmod(dir, 0o700)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			log.Printf("[creds] u%d %s: %v", userID, name, err)
		}
	}
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("# Доступы рабочего места (agctl). Подгрузить: set -a; . \"$AGCTL_CREDENTIALS_ENV\"; set +a\n")
	for _, k := range names {
		fmt.Fprintf(&sb, "%s=%s\n", k, shellQuote(vals[k]))
	}
	write("credentials.env", sb.String())

	if u, b := vals[localbridge.EnvURL], vals[localbridge.EnvToken]; u != "" && b != "" {
		raw, _ := json.Marshal(map[string]string{"url": u, "token": b})
		write("bridge.json", string(raw))
	}

	hasGit := false
	if host, tok := gitlabHost(vals["GITLAB_URL"]), vals["GITLAB_TOKEN"]; host != "" && tok != "" {
		hasGit = true
		scheme := "https"
		if strings.HasPrefix(vals["GITLAB_URL"], "http://") {
			scheme = "http"
		}
		write("git-credentials", fmt.Sprintf("%s://oauth2:%s@%s\n", scheme, url.PathEscape(tok), host))
		write("gitconfig", fmt.Sprintf("# agctl: git push в GitLab по личному токену разработчика\n[credential]\n\thelper = store --file=%s/%s/git-credentials\n", containerDshHome, agentCredsDir))
		if err := os.MkdirAll(filepath.Join(dir, "glab"), 0o700); err == nil {
			glab := fmt.Sprintf("hosts:\n    %s:\n        token: %s\n        api_host: %s\n        git_protocol: %s\n        api_protocol: %s\n", host, yamlQuote(tok), host, scheme, scheme)
			if err := os.WriteFile(filepath.Join(dir, "glab", "config.yml"), []byte(glab), 0o600); err != nil {
				log.Printf("[creds] u%d glab: %v", userID, err)
			}
		}
	}
	s.writeAccessSkill(userID, vals, hasGit)
	return agentCredsEnv(hasGit)
}

// writeAccessSkill — скилл агента: какие доступы настроены и как ими
// пользоваться (значения в скилл не попадают — только имена).
func (s *Server) writeAccessSkill(userID int64, vals map[string]string, hasGit bool) {
	dir := filepath.Join(s.projectDshHome(userID), "skills", "agctl-workspace-access")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	var sb strings.Builder
	sb.WriteString("---\nname: agctl-workspace-access\ndescription: Доступы рабочего места к GitLab, Jira и другим сервисам организации — где лежат и как их использовать в командах. Используй перед работой с GitLab/Jira из терминала.\n---\n\n")
	sb.WriteString("# Доступы рабочего места\n\nПеременные с KEY/TOKEN/SECRET/PASSWORD в имени в твой терминал не передаются\n(защита dsh). Доступы лежат в файле `$AGCTL_CREDENTIALS_ENV` (только для тебя,\nне копируй его в репозитории и не печатай значения). Подгрузить в команду:\n\n```\nset -a; . \"$AGCTL_CREDENTIALS_ENV\"; set +a; <команда>\n```\n\nЗаданы: ")
	names := make([]string, 0, len(vals))
	for k := range vals {
		names = append(names, "`"+k+"`")
	}
	sort.Strings(names)
	sb.WriteString(strings.Join(names, ", ") + ".\n\n")
	if hasGit {
		sb.WriteString("GitLab уже настроен: `git clone/push` по https на " + gitlabHost(vals["GITLAB_URL"]) + " и `glab` работают без\nввода токена. Работай в ветках `agent/*`, изменения — через merge request; в\nmain не пушь.\n\n")
	}
	if vals["JIRA_URL"] != "" {
		sb.WriteString("Jira: " + vals["JIRA_URL"] + ". Удобнее всего — MCP-сервер Jira (консоль воркспейса → MCP);\nиз терминала — REST API с токеном из файла доступов.\n")
	}
	_ = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(sb.String()), 0o644)
}

func gitlabHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

func yamlQuote(v string) string {
	raw, _ := json.Marshal(v) // JSON-строка — валидный YAML-скаляр
	return string(raw)
}
