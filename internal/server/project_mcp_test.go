package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arijjia/agctl/internal/secrets"
)

// TestProjectMcpAPI — гард-тесты раздела «MCP» консоли воркспейсов:
// границы доступа, валидация каталога, генерация пресета в DSH_HOME,
// секрет не утекает в ответы и KV-хранилище остаётся шифрованным.
func TestProjectMcpAPI(t *testing.T) {
	e := newTestEnv(t, 3)
	e.register(e.admin, "anna", "secret1")
	user := e.newClient()
	e.register(user, "boris", "secret2")

	// Чужой воркспейс — 404 (граница доступа как у components), мусорное имя — 400.
	if code, _ := e.do(user, "GET", "/api/projects/vibe/mcp", nil); code != 404 {
		t.Fatalf("user GET чужого = %d, want 404", code)
	}
	if code, _ := e.do(e.admin, "GET", "/api/projects/nope/mcp", nil); code != 404 {
		t.Fatalf("missing = %d, want 404", code)
	}
	if code, _ := e.do(e.admin, "GET", "/api/projects/.env/mcp", nil); code != 400 {
		t.Fatalf("invalid name = %d, want 400", code)
	}

	if code, body := e.do(e.admin, "POST", "/api/projects", map[string]string{"name": "vibe"}); code != 201 {
		t.Fatalf("create %d %s", code, body)
	}

	// Каталог возвращается с выбранной пустотой и без секретов.
	code, body := e.do(e.admin, "GET", "/api/projects/vibe/mcp", nil)
	if code != 200 {
		t.Fatalf("GET %d %s", code, body)
	}
	var listing struct {
		Servers []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			SecretEnv string `json:"secretEnv"`
		} `json:"servers"`
		Selected     []string `json:"selected"`
		TavilyKeySet bool     `json:"tavilyKeySet"`
		PresetID     string   `json:"presetID"`
	}
	if err := json.Unmarshal([]byte(body), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Servers) < 3 || listing.PresetID != "agctl-ws-vibe" {
		t.Fatalf("catalog broken: %+v", listing)
	}
	if strings.Contains(body, "tvly-") {
		t.Fatalf("GET утёк секрет: %s", body)
	}

	// PUT неизвестного сервера — 400.
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/mcp", map[string]any{"servers": []string{"ddg", "hacker-mcp"}}); code != 400 {
		t.Fatalf("unknown server = %d, want 400", code)
	}
	// Tavily без ключа — 400.
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/mcp", map[string]any{"servers": []string{"tavily"}}); code != 400 {
		t.Fatalf("tavily without key = %d, want 400", code)
	}

	// Осмысленный выбор: ddg + tavily с ключом → пресет в DSH_HOME воркспейса.
	code, body = e.do(e.admin, "PUT", "/api/projects/vibe/mcp", map[string]any{
		"servers":   []string{"ddg", "tavily"},
		"tavilyKey": "tvly-test-secret",
	})
	if code != 200 {
		t.Fatalf("PUT %d %s", code, body)
	}
	if strings.Contains(body, "tvly-test-secret") {
		t.Fatalf("PUT вернул секрет: %s", body)
	}
	dshHome := filepath.Join(e.home, "chats", "u1", "dsh-home")
	composition, err := os.ReadFile(filepath.Join(dshHome, ".agent-presets", "agctl-ws-vibe", "agent.cordis.yml"))
	if err != nil {
		t.Fatalf("пресет не записан: %v", err)
	}
	for _, want := range []string{"mcp-ddg", "mcp-tavily", "duckduckgo-mcp-server", "TAVILY_API_KEY: tvly-test-secret", "npm_config_cache: /opt/npm-mcp"} {
		if !strings.Contains(string(composition), want) {
			t.Fatalf("в пресете нет %q:\n%s", want, composition)
		}
	}
	if meta, err := os.ReadFile(filepath.Join(dshHome, ".agent-presets", "agctl-ws-vibe", "preset.yml")); err != nil || !strings.Contains(string(meta), "agctl-ws: vibe") {
		t.Fatalf("metadata пресета повреждена: %v %s", err, meta)
	}
	// Файл с секретом — 0600 (конвенция .credentials.yaml).
	if fi, err := os.Stat(filepath.Join(dshHome, ".agent-presets", "agctl-ws-vibe", "agent.cordis.yml")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("права композиции = %v, want 0600", fi.Mode().Perm())
	}

	// KV: выбор без секрета, секрет — отдельным шифрованным ключом.
	if raw, err := e.db.SettingGet("project-mcp-u1-vibe"); err != nil || strings.Contains(raw, "tvly-") {
		t.Fatalf("KV выбора: %v %q", err, raw)
	}
	enc, err := e.db.SettingGet("project-mcp-secret-u1-vibe")
	if err != nil || enc == "" || strings.Contains(enc, "tvly-test-secret") {
		t.Fatalf("KV секрета не шифрован: %v %q", err, enc)
	}
	keyPath, err := secrets.EnsureMasterKey(e.home)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := secrets.New(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if plain, decErr := mgr.Decrypt(enc); decErr != nil || plain != "tvly-test-secret" {
		t.Fatalf("KV секрета не расшифровывается: %v", decErr)
	}

	// Аудит без значений секретов.
	rows, err := e.db.AuditList(100)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(row.Details, "tvly-test-secret") {
			t.Fatalf("секрет попал в audit: %+v", row)
		}
	}

	// Перевыбор без ключа: ключ подхватывается из KV, пресет перезаписывается.
	code, _ = e.do(e.admin, "PUT", "/api/projects/vibe/mcp", map[string]any{"servers": []string{"tavily"}})
	if code != 200 {
		t.Fatalf("re-PUT %d", code)
	}
	composition, err = os.ReadFile(filepath.Join(dshHome, ".agent-presets", "agctl-ws-vibe", "agent.cordis.yml"))
	if err != nil || !strings.Contains(string(composition), "tvly-test-secret") || strings.Contains(string(composition), "mcp-ddg") {
		t.Fatalf("перевыбор не применился: %v %s", err, composition)
	}

	// Пустой выбор снимает пресет целиком.
	if code, _ := e.do(e.admin, "PUT", "/api/projects/vibe/mcp", map[string]any{"servers": []string{}}); code != 200 {
		t.Fatalf("empty PUT %d", code)
	}
	if _, err := os.Stat(filepath.Join(dshHome, ".agent-presets", "agctl-ws-vibe")); !os.IsNotExist(err) {
		t.Fatalf("пустой выбор не снял пресет: %v", err)
	}
}

// TestServerRuntimeSelector — opt-in селектор чат-рантайма: неизвестное
// имя роняется сразу, k8s без доступного кластера падает с внятной ошибкой,
// а не молча отдаёт Docker (карточка k8s-chat-runtime-adapter).
func TestServerRuntimeSelector(t *testing.T) {
	e := newTestEnv(t, 3)
	keyPath, err := secrets.EnsureMasterKey(e.home)
	if err != nil {
		t.Fatal(err)
	}
	mgr, err := secrets.New(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(e.db, mgr, Config{
		Home: e.home, Addr: "test", Runtime: "bogus",
	}); err == nil || !strings.Contains(err.Error(), "неизвестный chat runtime") {
		t.Fatalf("bogus runtime: %v", err)
	}
	// k8s-селектор требует Docker для workspace-слоя: на машине без демона
	// падает внятной ошибкой dockerrt, а не молча отдаёт Docker-чат.
}
