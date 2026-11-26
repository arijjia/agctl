package provision

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// standardFixture — срез presets/standard.patch.yml dsh: строка-декларация с
// `!!js`-условиями и skill-filesystem, как в установленном бандле.
const standardFixture = `# Agent preset standard
- insert:
    - id: preset-standard
      name: '@deepseek-ai/dsh-agent-preset'
      config:
        id: standard
        order: 1
        plugins:
          - id: persona
            name: '@deepseek-ai/dsh-persona'
            config:
              prefix: You are a coding agent.
          - id: tool-bash
            name: '@deepseek-ai/dsh-tool-bash'
            disabled: !!js process.platform === 'win32'
          - id: skill-filesystem
            name: '@deepseek-ai/dsh-skill-filesystem'
            config:
              customSkillDirs:
                - !!js dshSkills()
`

// Пресет лаунчера превращается в строку-декларацию: состав standard + его
// MCP-ряды + его каталог скиллов; имя с «: » и кириллицей остаётся валидным
// YAML; `!!js` штатного состава не теряется.
func TestSyncPresetPatch(t *testing.T) {
	home := t.TempDir()
	spec, _ := PlatformMcpServer("ddg")
	if err := EnsureAgentPreset(home, "agctl-demo", "Демо: поиск", "набор: \"поиск\"", []McpServerSpec{spec}, nil); err != nil {
		t.Fatal(err)
	}
	// Пресет только со скиллами: MCP нет, каталог пресета всё равно есть.
	if err := EnsureAgentPreset(home, "agctl-skills", "Скиллы", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(PresetSkillsRoot(home, "agctl-skills"), "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ручной пресет пользователя agctl не трогает.
	if err := os.MkdirAll(filepath.Join(home, ".agent-presets", "mine"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, n, err := SyncPresetPatch(home, "/home/dsh", []byte(standardFixture))
	if err != nil || n != 2 {
		t.Fatalf("SyncPresetPatch: n=%d err=%v", n, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("права патча %v, нужно 0600", fi.Mode().Perm())
	}
	text := string(raw)
	for _, want := range []string{"!!js process.platform === 'win32'", "!!js dshSkills()"} {
		if !strings.Contains(text, want) {
			t.Fatalf("патч потерял %q:\n%s", want, text)
		}
	}
	var patch []struct {
		Insert []struct {
			ID     string `yaml:"id"`
			Name   string `yaml:"name"`
			Config struct {
				ID          string      `yaml:"id"`
				Name        string      `yaml:"name"`
				Description string      `yaml:"description"`
				Order       int         `yaml:"order"`
				Plugins     []yaml.Node `yaml:"plugins"`
			} `yaml:"config"`
		} `yaml:"insert"`
	}
	if err := yaml.Unmarshal(raw, &patch); err != nil {
		t.Fatalf("патч не разбирается: %v\n%s", err, text)
	}
	rows := patch[0].Insert
	if len(rows) != 2 || rows[0].ID != "preset-agctl-demo" || rows[0].Name != agentPresetPlugin {
		t.Fatalf("строки: %+v", rows)
	}
	demo := rows[0].Config
	if demo.ID != "agctl-demo" || demo.Name != "Демо: поиск" || demo.Description != "набор: \"поиск\"" || demo.Order != presetOrderBase {
		t.Fatalf("декларация demo: %+v", demo)
	}
	if len(demo.Plugins) != 4 { // 3 штатных + ряд MCP
		t.Fatalf("состав demo: %d строк\n%s", len(demo.Plugins), text)
	}
	if !strings.Contains(text, "serverName: "+spec.ServerName) {
		t.Fatalf("нет MCP-ряда:\n%s", text)
	}
	skills := rows[1].Config
	if len(skills.Plugins) != 3 || !strings.Contains(text, "/home/dsh/.agent-presets/agctl-skills/skills") {
		t.Fatalf("пресет со скиллами: %d строк\n%s", len(skills.Plugins), text)
	}
	if strings.Contains(text, "preset-mine") {
		t.Fatal("ручной пресет попал в патч agctl")
	}

	// Пресетов не осталось — патч убирается, dsh стартует без --patch.
	for _, id := range []string{"agctl-demo", "agctl-skills"} {
		if err := RemoveMcpPreset(home, id); err != nil {
			t.Fatal(err)
		}
	}
	if path, n, err := SyncPresetPatch(home, "/home/dsh", []byte(standardFixture)); err != nil || n != 0 || path != "" {
		t.Fatalf("после снятия: path=%q n=%d err=%v", path, n, err)
	}
	if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(PresetPatchRel))); !os.IsNotExist(err) {
		t.Fatal("патч не удалён")
	}
}

// Сменился формат бандла dsh — сборка падает с понятной причиной, а не
// молча выдаёт пресеты без инструментов.
func TestStandardPresetPluginsMissing(t *testing.T) {
	if _, err := StandardPresetPlugins([]byte("- insert:\n    - id: other\n")); err == nil {
		t.Fatal("ожидалась ошибка: нет preset-standard")
	}
}
