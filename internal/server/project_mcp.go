package server

// project_mcp.go — раздел «MCP» консоли воркспейсов (карточка workspace-mcp-config):
// включение/выключение серверов платформенного каталога для конкретного
// воркспейса. agctl генерирует per-workspace dsh-пресет в DSH_HOME воркспейса
// (provision.EnsureWorkspaceMcpPreset); секрет провайдера (ключ Tavily)
// хранится в зашифрованном KV и пишется только в файл пресета 0600 — не в
// рабочее дерево, не в ответы API и не в журнал аудита.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/arijjia/agctl/internal/provision"
)

const maxMcpBodyBytes = 16 << 10 // выбор серверов + один ключ — небольшое тело

// mcpSettingKey — ключ KV с последним применённым выбором (без секретов).
func mcpSettingKey(userID int64, project string) string {
	return "project-mcp-u" + strconv.FormatInt(userID, 10) + "-" + project
}

// mcpSecretSettingKey — ключ KV с шифрованным секретом провайдера MCP.
func mcpSecretSettingKey(userID int64, project string) string {
	return "project-mcp-secret-u" + strconv.FormatInt(userID, 10) + "-" + project
}

// projectDshHome — DSH_HOME владельца воркспейса (персональный том контейнера).
func (s *Server) projectDshHome(userID int64) string {
	return filepath.Join(s.cfg.Home, "chats", chatStorageID(userID), "dsh-home")
}

// handleProjectMcpGet — GET /api/projects/{name}/mcp: каталог платформы +
// текущий выбор + флаг «ключ задан». Значение ключа не возвращается.
func (s *Server) handleProjectMcpGet(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("name")
	if _, code, msg := s.resolveProjectDir(id.ID, name); code != 0 {
		writeErr(w, code, msg)
		return
	}
	var saved struct {
		Servers []string `json:"servers"`
	}
	raw, err := s.db.SettingGet(mcpSettingKey(id.ID, name))
	if err == nil && raw != "" {
		if decErr := json.Unmarshal([]byte(raw), &saved); decErr != nil {
			log.Printf("[mcp] u%d %s: сохранённый выбор повреждён — игнорирую", id.ID, name)
		}
	}
	keySet := false
	if enc, err := s.db.SettingGet(mcpSecretSettingKey(id.ID, name)); err == nil && enc != "" {
		if plain, decErr := s.sec.Decrypt(enc); decErr == nil && plain != "" {
			keySet = true
		}
	}
	servers := make([]map[string]any, 0, len(provision.PlatformMcpServers))
	for _, spec := range provision.PlatformMcpServers {
		if spec.LocalOnly && !s.isLocal() {
			continue
		}
		servers = append(servers, map[string]any{
			"id":          spec.ID,
			"title":       spec.Title,
			"description": spec.Description,
			"secretEnv":   spec.SecretEnv,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"servers":      servers,
		"selected":     normalizeDisabledIDs(saved.Servers),
		"tavilyKeySet": keySet,
		"presetID":     provision.WorkspaceMcpPresetID(name),
	})
}

// handleProjectMcpPut — PUT /api/projects/{name}/mcp с телом
// {"servers":[...], "tavilyKey": "..."}: валидация по каталогу, генерация
// пресета в DSH_HOME воркспейса, KV-выбор (секрет — зашифрованным отдельным
// ключом) и запись в audit без значений секретов.
func (s *Server) handleProjectMcpPut(w http.ResponseWriter, r *http.Request, id identity) {
	name := r.PathValue("name")
	dshHome := s.projectDshHome(id.ID) // создаётся лениво EnsureChat'ом; пресет переживёт посев
	var req struct {
		Servers   []string `json:"servers"`
		TavilyKey string   `json:"tavilyKey"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMcpBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "неверный JSON выбора MCP")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeErr(w, http.StatusBadRequest, "ожидался один JSON объект")
		return
	}
	selected := normalizeDisabledIDs(req.Servers)
	specs := make([]provision.McpServerSpec, 0, len(selected))
	needSecret := false
	for _, sid := range selected {
		spec, ok := provision.PlatformMcpServer(sid)
		if ok && spec.LocalOnly && !s.isLocal() {
			ok = false
		}
		if !ok {
			writeErr(w, http.StatusBadRequest, "неизвестный MCP-сервер: "+sid)
			return
		}
		if spec.SecretEnv != "" {
			needSecret = true
		}
		specs = append(specs, spec)
	}
	secretEnv := map[string]string{}
	if needSecret {
		key := req.TavilyKey
		if key == "" {
			// Ключ мог быть сохранён раньше: перезаливать не обязаны.
			if enc, err := s.db.SettingGet(mcpSecretSettingKey(id.ID, name)); err == nil && enc != "" {
				if plain, decErr := s.sec.Decrypt(enc); decErr == nil {
					key = plain
				}
			}
		}
		if key == "" {
			writeErr(w, http.StatusBadRequest, "для Tavily нужен ключ TAVILY_API_KEY")
			return
		}
		secretEnv["TAVILY_API_KEY"] = key
		if enc, encErr := s.sec.Encrypt(key); encErr == nil {
			if err := s.db.SettingSet(mcpSecretSettingKey(id.ID, name), enc); err != nil {
				log.Printf("[mcp] u%d %s: ключ не сохранён в KV", id.ID, name)
			}
		}
	}
	// Доступы рабочего места (GitLab/Jira/мост) — из того же env, что
	// получает контейнер пользователя: серверам каталога с PassEnv.
	for k, v := range s.containerEnvMap(id.ID) {
		if _, taken := secretEnv[k]; !taken {
			secretEnv[k] = v
		}
	}
	if err := provision.EnsureWorkspaceMcpPreset(dshHome, name, specs, secretEnv); err != nil {
		log.Printf("[mcp] u%d %s: пресет не записан: %v", id.ID, name, err)
		writeErr(w, http.StatusInternalServerError, "не удалось записать MCP-пресет воркспейса")
		return
	}
	choice, encErr := json.Marshal(map[string]any{"servers": selected})
	if encErr == nil {
		if err := s.db.SettingSet(mcpSettingKey(id.ID, name), string(choice)); err != nil {
			log.Printf("[mcp] u%d %s: выбор не сохранён в KV", id.ID, name)
		}
	}
	_ = s.db.Audit(id.Login, "project.mcp.update", name, "servers="+joinIDs(selected))
	s.handleProjectMcpGet(w, r, id)
}

// joinIDs — компактная строка для audit.
func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += id
	}
	return out
}

// containerEnvMap — env рабочего места пользователя (доменные доступы
// developer'а и, в локальном режиме, секреты из хранилища ОС + мост).
func (s *Server) containerEnvMap(userID int64) map[string]string {
	out := map[string]string{}
	pairs := append([]string(nil), s.developerEnv(userID)...)
	if _, env := s.localChatExtras(userID); len(env) > 0 {
		pairs = append(pairs, env...)
	}
	for _, kv := range pairs {
		if k, v, ok := strings.Cut(kv, "="); ok {
			out[k] = v
		}
	}
	return out
}
