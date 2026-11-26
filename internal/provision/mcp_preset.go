package provision

// mcp_preset.go — per-workspace MCP-пресет dsh (карточка workspace-mcp-config).
// agctl пишет в DSH_HOME воркспейса пресет .agent-presets/agctl-ws-<project>/
// с ровно выбранными в консоли рядами @deepseek-ai/dsh-mcp-client: сессии
// воркспейса, созданные с этим пресетом, получают выбранный набор серверов.
// Пресет живёт в DSH_HOME — вне Git и рабочего дерева; секретные значения
// (ключи провайдеров) попадают только в файл 0600, не в KV-журнал и ответы API.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// McpServerSpec — запись платформенного каталога MCP-серверов.
type McpServerSpec struct {
	ID          string   // идентификатор выбора в консоли (валидация PUT)
	Title       string   // человекочитаемое имя в консоли
	Description string   // подсказка в консоли
	ServerName  string   // serverName ряда cordis
	Args        []string // аргументы npx после -y (или Command)
	SecretEnv   string   // "" — сервер без ключа; иначе имя env-переменной
	// Command — исполняемый файл сервера; пусто = npx (с -y).
	Command string
	// PassEnv — переменные рабочего места (доступы GitLab/Jira, мост),
	// которые нужны серверу: значения берутся из env контейнера
	// пользователя и пишутся в пресет 0600. Нет значения — переменная не
	// передаётся (сервер сообщит, чего не хватает).
	PassEnv []string
	// LocalOnly — только в локальном режиме (agctl dev): мост к хосту.
	LocalOnly bool
	// NeedEnv — доступы, без которых сервер не запустится: каждое значение
	// должно быть задано; «A|B» — достаточно одного из вариантов. Без них
	// строка пресета отключается (MissingEnv) — иначе dsh ждал бы сервер
	// при старте и рабочее место не поднималось бы.
	NeedEnv []string
}

// MissingEnv — каких доступов не хватает серверу (по NeedEnv и SecretEnv).
func (s McpServerSpec) MissingEnv(env map[string]string) []string {
	var out []string
	need := append([]string(nil), s.NeedEnv...)
	if s.SecretEnv != "" {
		need = append(need, s.SecretEnv)
	}
	for _, alt := range need {
		ok := false
		for _, name := range strings.Split(alt, "|") {
			ok = ok || strings.TrimSpace(env[strings.TrimSpace(name)]) != ""
		}
		if !ok {
			out = append(out, alt)
		}
	}
	return out
}

// mcpStartupTimeoutMs — предел на запуск MCP-сервера и первичную загрузку
// его инструментов. dsh активирует пресеты при старте и без предела ждёт
// зависший сервер бесконечно; по истечении сервер пропускается с ошибкой в
// логе, остальное рабочее место работает.
const mcpStartupTimeoutMs = 45000

// PlatformMcpServers — каталог MCP-серверов платформы. Список согласован с
// прогревом npx-кэша образа agctl-dsh (Dockerfile, слой mcp-warm): новый
// сервер добавляется вместе с прогревом, чтобы первая сессия не тянула сеть.
var PlatformMcpServers = []McpServerSpec{
	{ID: "ddg", Title: "DuckDuckGo", Description: "Веб-поиск, без ключа", ServerName: "ddg", Args: []string{"duckduckgo-mcp-server"}},
	{ID: "context7", Title: "Context7", Description: "Документация библиотек, без ключа", ServerName: "context7", Args: []string{"@upstash/context7-mcp"}},
	{ID: "tavily", Title: "Tavily", Description: "Веб-поиск, нужен ключ TAVILY_API_KEY", ServerName: "tavily", Args: []string{"tavily-mcp"}, SecretEnv: "TAVILY_API_KEY"},
	{ID: "gitlab", Title: "GitLab", Description: "Задачи, MR, пайплайны, файлы репозиториев; доступ — личный токен GitLab разработчика", ServerName: "gitlab",
		Args: []string{"@zereight/mcp-gitlab@2.3.27"}, PassEnv: []string{"GITLAB_PERSONAL_ACCESS_TOKEN", "GITLAB_API_URL"},
		NeedEnv: []string{"GITLAB_PERSONAL_ACCESS_TOKEN"}},
	{ID: "jira", Title: "Jira", Description: "Задачи и комментарии Jira (Server/DC — личный токен, Cloud — логин+API-токен); ставится при первом запуске через uvx", ServerName: "jira",
		Command: "uvx", Args: []string{"mcp-atlassian==0.24.4"},
		PassEnv: []string{"JIRA_URL", "JIRA_PERSONAL_TOKEN", "JIRA_USERNAME", "JIRA_API_TOKEN", "UV_INDEX_URL"},
		NeedEnv: []string{"JIRA_URL", "JIRA_PERSONAL_TOKEN|JIRA_API_TOKEN"}},
	{ID: "playwright", Title: "Playwright", Description: "Браузер для агента: открыть страницу, кликнуть, скриншот, e2e; нужен образ с браузерами (вкус web)", ServerName: "playwright",
		Args: []string{"@playwright/mcp@0.0.85", "--headless", "--isolated", "--no-sandbox", "--browser", "chromium"}},
	{ID: "host-bridge", Title: "Мост к хосту", Description: "Разрешённые команды на компьютере разработчика (белый список agctl dev)", ServerName: "host-bridge",
		Command: "agctl", Args: []string{"bridge", "mcp"}, PassEnv: []string{"AGCTL_BRIDGE_URL", "AGCTL_BRIDGE_TOKEN"}, LocalOnly: true,
		NeedEnv: []string{"AGCTL_BRIDGE_URL", "AGCTL_BRIDGE_TOKEN"}},
}

// PlatformMcpServer — выбор записи каталога по id; ok=false для чужого id.
func PlatformMcpServer(id string) (McpServerSpec, bool) {
	for _, s := range PlatformMcpServers {
		if s.ID == id {
			return s, true
		}
	}
	return McpServerSpec{}, false
}

// WorkspaceMcpPresetID — id пресета воркспейса в dsh-roster: agctl-ws-<project>.
// Имя проекта уже валидировано ValidProjectName ([a-z0-9-_]), поэтому
// безопасно и как имя каталога.
func WorkspaceMcpPresetID(project string) string {
	return "agctl-ws-" + project
}

// WorkspaceMcpPresetDir — каталог пресета внутри DSH_HOME воркспейса.
func WorkspaceMcpPresetDir(dshHome, project string) string {
	return filepath.Join(dshHome, ".agent-presets", WorkspaceMcpPresetID(project))
}

// mcpCordisRow — ряд agent-композиции dsh (форма живого провода: пресеты
// .agent-presets/universal/agent.cordis.yml, ряды mcp-web-*).
type mcpCordisRow struct {
	ID      string         `yaml:"id"`
	Name    string         `yaml:"name"`
	Enabled *bool          `yaml:"disabled,omitempty"`
	Config  map[string]any `yaml:"config"`
}

// EnsureWorkspaceMcpPreset перезаписывает пресет воркспейса выбранным составом
// (истина — файл, повторный вызов идемпотентен). secretEnv несёт значения
// секретных env-переменных выбранного состава. Пустой выбор снимает пресет.
func EnsureWorkspaceMcpPreset(dshHome, project string, specs []McpServerSpec, secretEnv map[string]string) error {
	return EnsureMcpPreset(dshHome, WorkspaceMcpPresetID(project), "Воркспейс "+project,
		fmt.Sprintf("MCP-набор воркспейса %q из консоли agctl — не редактируйте вручную, выбор в консоли перезаписывает файл.", project),
		specs, secretEnv)
}

// EnsureMcpPreset — писатель MCP-пресета воркспейса: пустой выбор снимает
// пресет (сессии воркспейса больше не должны видеть MCP-ряды), иначе —
// EnsureAgentPreset.
func EnsureMcpPreset(dshHome, presetID, name, description string, specs []McpServerSpec, secretEnv map[string]string) error {
	if len(specs) == 0 {
		return RemoveMcpPreset(dshHome, presetID)
	}
	return EnsureAgentPreset(dshHome, presetID, name, description, specs, secretEnv)
}

// EnsureAgentPreset — общий писатель управляемых пресетов agctl: каталог
// .agent-presets/<presetID>/ с preset.yml (имя и описание в ростере dsh) и
// agent.cordis.yml (MCP-ряды; пустой список — пресет только со скиллами).
// Сам dsh этот каталог не читает: перед стартом guest собирает из него
// патч-слой (preset_patch.go). Префикс «agctl-» в presetID — марка
// управляемого пресета: ручные пресеты dsh agctl не трогает. Идемпотентно.
func EnsureAgentPreset(dshHome, presetID, name, description string, specs []McpServerSpec, secretEnv map[string]string) error {
	dir := filepath.Join(dshHome, ".agent-presets", presetID)
	rows := make([]mcpCordisRow, 0, len(specs))
	for _, s := range specs {
		env := map[string]string{"npm_config_cache": "/opt/npm-mcp"}
		if s.SecretEnv != "" {
			if v := secretEnv[s.SecretEnv]; v != "" {
				env[s.SecretEnv] = v
			}
		}
		for _, name := range s.PassEnv {
			if v := secretEnv[name]; v != "" {
				env[name] = v
			}
		}
		command, args := "npx", append([]string{"-y"}, s.Args...)
		if s.Command != "" {
			command, args = s.Command, append([]string(nil), s.Args...)
		}
		row := mcpCordisRow{
			ID:   "mcp-" + s.ServerName,
			Name: "@deepseek-ai/dsh-mcp-client",
			Config: map[string]any{
				"serverName":       s.ServerName,
				"transport":        "stdio",
				"command":          command,
				"args":             args,
				"env":              env,
				"startupTimeoutMs": mcpStartupTimeoutMs,
			},
		}
		if missing := s.MissingEnv(secretEnv); len(missing) > 0 {
			off := true
			row.Enabled = &off // поле yaml:"disabled": сервер без доступов не запускается
		}
		rows = append(rows, row)
	}
	composition, err := yaml.Marshal(rows)
	if err != nil {
		return fmt.Errorf("mcp preset marshal: %w", err)
	}
	// Каталог 0700 + композиция 0600: в файле могут быть ключи провайдеров,
	// конвенция как у .credentials.yaml dsh-home.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mcp preset mkdir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.cordis.yml"), composition, 0o600); err != nil {
		return fmt.Errorf("mcp preset write: %w", err)
	}
	// yaml.Marshal, а не Sprintf: имя и описание задаёт пользователь, в них
	// бывают «: », кавычки и переводы строк.
	meta, err := yaml.Marshal(presetMeta{Name: name, Description: description})
	if err != nil {
		return fmt.Errorf("mcp preset metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preset.yml"), meta, 0o644); err != nil {
		return fmt.Errorf("mcp preset metadata: %w", err)
	}
	return nil
}

// RemoveMcpPreset — снять agctl-пресет из dsh-home (удаление пресета в
// лаунчере). Отсутствие каталога — норма (идемпотентно).
func RemoveMcpPreset(dshHome, presetID string) error {
	if err := os.RemoveAll(filepath.Join(dshHome, ".agent-presets", presetID)); err != nil {
		return fmt.Errorf("mcp preset remove: %w", err)
	}
	return nil
}
